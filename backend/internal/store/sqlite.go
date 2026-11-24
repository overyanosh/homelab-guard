package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
	"homelab-guard/internal/crypto"
	_ "modernc.org/sqlite"
)

type Store struct {
	db    *sql.DB
	vault *crypto.Vault
}

type ProxmoxCluster struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Endpoint    string `json:"endpoint"`
	TokenID     string `json:"token_id"`
	TokenSecret string `json:"token_secret,omitempty"`
	InsecureTLS bool   `json:"insecure_tls"`
}

type SSHHost struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	PrivateKeyPEM string `json:"private_key_pem,omitempty"`
	PublicKey     string `json:"public_key,omitempty"`
}

type AgentNode struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	EnrollToken string `json:"enroll_token,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

func NewStore(dbPath string, vault *crypto.Vault) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS proxmox_clusters (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		endpoint TEXT NOT NULL,
		token_id TEXT NOT NULL,
		encrypted_secret TEXT NOT NULL,
		insecure_tls INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS ssh_hosts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		host TEXT NOT NULL,
		port INTEGER NOT NULL DEFAULT 22,
		username TEXT NOT NULL,
		encrypted_private_key TEXT NOT NULL,
		public_key TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS agent_nodes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		token_hash TEXT NOT NULL UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema migration failed: %w", err)
	}

	return &Store{db: db, vault: vault}, nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (s *Store) CreateAgentNode(name string) (*AgentNode, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	rawToken := "hlg_" + hex.EncodeToString(buf)
	res, err := s.db.Exec(`INSERT INTO agent_nodes (name, token_hash) VALUES (?, ?)`, name, hashToken(rawToken))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &AgentNode{ID: id, Name: name, EnrollToken: rawToken}, nil
}

func (s *Store) ValidateAgentToken(rawToken string) (*AgentNode, error) {
	var n AgentNode
	err := s.db.QueryRow(`SELECT id, name FROM agent_nodes WHERE token_hash = ?`, hashToken(rawToken)).Scan(&n.ID, &n.Name)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) ListAgentNodes() ([]AgentNode, error) {
	rows, err := s.db.Query(`SELECT id, name, created_at FROM agent_nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []AgentNode
	for rows.Next() {
		var n AgentNode
		if err := rows.Scan(&n.ID, &n.Name, &n.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, n)
	}
	return list, nil
}

func GenerateEd25519KeyPair(comment string) (privatePEM string, authorizedKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	pemBlock, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return "", "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(pemBlock)), string(ssh.MarshalAuthorizedKey(sshPub)), nil
}

func (s *Store) SaveSSHHost(h SSHHost) (int64, string, error) {
	privPEM := h.PrivateKeyPEM
	pubKey := h.PublicKey
	if privPEM == "" {
		var err error
		privPEM, pubKey, err = GenerateEd25519KeyPair("homelab-guard@" + h.Name)
		if err != nil {
			return 0, "", err
		}
	}
	if h.Port == 0 {
		h.Port = 22
	}
	encKey, err := s.vault.Encrypt([]byte(privPEM))
	if err != nil {
		return 0, "", err
	}
	res, err := s.db.Exec(
		`INSERT INTO ssh_hosts (name, host, port, username, encrypted_private_key, public_key) VALUES (?, ?, ?, ?, ?, ?)`,
		h.Name, h.Host, h.Port, h.Username, encKey, pubKey,
	)
	if err != nil {
		return 0, "", err
	}
	id, _ := res.LastInsertId()
	return id, pubKey, nil
}

func (s *Store) GetSSHHost(id int64) (*SSHHost, error) {
	var h SSHHost
	var encKey string
	err := s.db.QueryRow(
		`SELECT id, name, host, port, username, encrypted_private_key, COALESCE(public_key, '') FROM ssh_hosts WHERE id = ?`, id,
	).Scan(&h.ID, &h.Name, &h.Host, &h.Port, &h.Username, &encKey, &h.PublicKey)
	if err != nil {
		return nil, err
	}
	plainKey, err := s.vault.Decrypt(encKey)
	if err != nil {
		return nil, err
	}
	h.PrivateKeyPEM = string(plainKey)
	return &h, nil
}

func (s *Store) ListSSHHosts() ([]SSHHost, error) {
	rows, err := s.db.Query(`SELECT id, name, host, port, username, COALESCE(public_key, '') FROM ssh_hosts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []SSHHost
	for rows.Next() {
		var h SSHHost
		if err := rows.Scan(&h.ID, &h.Name, &h.Host, &h.Port, &h.Username, &h.PublicKey); err != nil {
			return nil, err
		}
		list = append(list, h)
	}
	return list, nil
}

func (s *Store) SaveProxmoxCluster(c ProxmoxCluster) (int64, error) {
	encSecret, err := s.vault.Encrypt([]byte(c.TokenSecret))
	if err != nil {
		return 0, err
	}
	insecure := 0
	if c.InsecureTLS {
		insecure = 1
	}

	res, err := s.db.Exec(
		`INSERT INTO proxmox_clusters (name, endpoint, token_id, encrypted_secret, insecure_tls) VALUES (?, ?, ?, ?, ?)`,
		c.Name, c.Endpoint, c.TokenID, encSecret, insecure,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetProxmoxCluster(id int64) (*ProxmoxCluster, error) {
	var c ProxmoxCluster
	var encSecret string
	var insecure int

	err := s.db.QueryRow(
		`SELECT id, name, endpoint, token_id, encrypted_secret, insecure_tls FROM proxmox_clusters WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Endpoint, &c.TokenID, &encSecret, &insecure)
	if err != nil {
		return nil, err
	}

	plainSecret, err := s.vault.Decrypt(encSecret)
	if err != nil {
		return nil, err
	}
	c.TokenSecret = string(plainSecret)
	c.InsecureTLS = insecure == 1
	return &c, nil
}

func (s *Store) ListProxmoxClusters() ([]ProxmoxCluster, error) {
	rows, err := s.db.Query(`SELECT id, name, endpoint, token_id, insecure_tls FROM proxmox_clusters ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ProxmoxCluster
	for rows.Next() {
		var c ProxmoxCluster
		var insecure int
		if err := rows.Scan(&c.ID, &c.Name, &c.Endpoint, &c.TokenID, &insecure); err != nil {
			return nil, err
		}
		c.InsecureTLS = insecure == 1
		list = append(list, c)
	}
	return list, nil
}
