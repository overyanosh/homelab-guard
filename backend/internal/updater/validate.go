package updater

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/distribution/reference"
	"gopkg.in/yaml.v3"
)

// ValidationIssue is a single problem found in a compose definition.
type ValidationIssue struct {
	Severity string `json:"severity"` // "error" | "warning"
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Field    string `json:"field,omitempty"`
	Message  string `json:"message"`
}

// ValidationResult is the outcome of validating a compose document.
type ValidationResult struct {
	Valid    bool              `json:"valid"`
	Errors   []ValidationIssue `json:"errors"`
	Warnings []ValidationIssue `json:"warnings"`
	Services []string          `json:"services"`
	// MissingEnv lists variables referenced by the compose but absent from .env.
	MissingEnv []string `json:"missing_env,omitempty"`
	// EmptyEnv lists variables declared in .env but left without a value.
	EmptyEnv []string `json:"empty_env,omitempty"`
}

var (
	yamlLineRe      = regexp.MustCompile(`line (\d+)`)
	envKeyRe        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	unresolvedVarRe = regexp.MustCompile(`\$\{?[A-Za-z_]`)
)

// ValidateCompose parses and checks a compose document before deployment. It
// reports syntax errors (with line numbers) and semantic problems that would
// make the container fail to start.
func ValidateCompose(doc, dotEnv string) ValidationResult {
	res := ValidationResult{Errors: []ValidationIssue{}, Warnings: []ValidationIssue{}, Services: []string{}}
	errf := func(line, col int, field, msg string) {
		res.Errors = append(res.Errors, ValidationIssue{Severity: "error", Line: line, Column: col, Field: field, Message: msg})
	}
	warnf := func(line, col int, field, msg string) {
		res.Warnings = append(res.Warnings, ValidationIssue{Severity: "warning", Line: line, Column: col, Field: field, Message: msg})
	}

	if strings.TrimSpace(doc) == "" {
		errf(0, 0, "", "le compose est vide")
		res.Valid = false
		return res
	}

	// YAML forbids tabs for indentation; report them precisely, because the raw
	// parser error is otherwise hard to interpret.
	for i, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.Contains(line[:len(line)-len(trimmed)], "\t") {
			errf(i+1, 1, "", "tabulation dans l'indentation : YAML exige des espaces")
		} else if strings.TrimRight(line, " \t") != line {
			warnf(i+1, len(line), "", "espaces en fin de ligne")
		}
	}

	vars := ParseDotEnv(dotEnv)
	missing, empty := envGaps(doc, vars)
	res.MissingEnv = missing
	res.EmptyEnv = empty
	for _, name := range missing {
		warnf(0, 0, name, fmt.Sprintf("variable ${%s} référencée mais absente du .env", name))
	}
	for _, name := range empty {
		warnf(0, 0, name, fmt.Sprintf("variable ${%s} déclarée dans le .env sans valeur", name))
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &root); err != nil {
		line := 0
		if m := yamlLineRe.FindStringSubmatch(err.Error()); len(m) == 2 {
			line, _ = strconv.Atoi(m[1])
		}
		msg := strings.TrimPrefix(err.Error(), "yaml: ")

		// The parser often reports the line where it gave up rather than the
		// faulty one. Point at the first odd indentation, the usual culprit.
		if badLine, spaces := firstOddIndent(doc); badLine > 0 {
			errf(badLine, spaces+1, "", fmt.Sprintf(
				"indentation irrégulière : %d espaces (attendu un multiple de 2)", spaces))
			msg = "structure YAML invalide — " + msg
		}
		errf(line, 0, "", msg)
		res.Valid = false
		return res
	}
	if len(root.Content) == 0 {
		errf(0, 0, "", "document YAML vide")
		res.Valid = false
		return res
	}

	top := root.Content[0]
	if top.Kind != yaml.MappingNode {
		errf(top.Line, top.Column, "", "la racine du compose doit être un objet (mapping)")
		res.Valid = false
		return res
	}

	var servicesNode *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		key, val := top.Content[i], top.Content[i+1]
		switch key.Value {
		case "services":
			servicesNode = val
		case "version":
			warnf(key.Line, key.Column, "version", "la clé 'version' est obsolète et ignorée")
		case "volumes", "networks", "secrets", "configs", "name":
			// allowed at top level
		default:
			warnf(key.Line, key.Column, key.Value, "clé de premier niveau inconnue")
		}
	}

	if servicesNode == nil {
		errf(top.Line, top.Column, "services", "section 'services' manquante")
		res.Valid = false
		return res
	}
	if servicesNode.Kind != yaml.MappingNode || len(servicesNode.Content) == 0 {
		errf(servicesNode.Line, servicesNode.Column, "services", "la section 'services' doit contenir au moins un service")
		res.Valid = false
		return res
	}

	for i := 0; i+1 < len(servicesNode.Content); i += 2 {
		nameNode, svcNode := servicesNode.Content[i], servicesNode.Content[i+1]
		name := nameNode.Value
		res.Services = append(res.Services, name)

		if !validServiceName(name) {
			errf(nameNode.Line, nameNode.Column, name, "nom de service invalide (lettres, chiffres, '_', '.', '-')")
		}
		if svcNode.Kind != yaml.MappingNode {
			errf(svcNode.Line, svcNode.Column, name, "la définition du service doit être un objet")
			continue
		}

		fields := map[string]*yaml.Node{}
		for j := 0; j+1 < len(svcNode.Content); j += 2 {
			fields[svcNode.Content[j].Value] = svcNode.Content[j+1]
		}

		// image / build
		imageNode := fields["image"]
		if _, hasBuild := fields["build"]; hasBuild && imageNode == nil {
			errf(svcNode.Line, svcNode.Column, name+".build", "'build' n'est pas supporté : renseignez une 'image'")
		} else if imageNode == nil {
			errf(svcNode.Line, svcNode.Column, name+".image", "clé 'image' requise")
		} else {
			img := substituteEnv(strings.TrimSpace(imageNode.Value), vars)
			if img == "" {
				errf(imageNode.Line, imageNode.Column, name+".image", "image vide")
			} else if _, err := reference.ParseNormalizedNamed(img); err != nil {
				errf(imageNode.Line, imageNode.Column, name+".image", fmt.Sprintf("référence d'image invalide : %v", err))
			} else if unresolvedVarRe.MatchString(img) {
				warnf(imageNode.Line, imageNode.Column, name+".image", "variable non résolue dans l'image")
			}
		}

		// container_name
		if cn := fields["container_name"]; cn != nil {
			val := substituteEnv(strings.TrimSpace(cn.Value), vars)
			if !validContainerName(val) {
				errf(cn.Line, cn.Column, name+".container_name", "nom de conteneur invalide")
			}
		}

		// restart
		if rn := fields["restart"]; rn != nil {
			allowed := map[string]bool{"no": true, "always": true, "on-failure": true, "unless-stopped": true}
			val := strings.TrimSpace(rn.Value)
			if !allowed[val] {
				errf(rn.Line, rn.Column, name+".restart", "valeur invalide (no, always, on-failure, unless-stopped)")
			}
		}

		// ports
		if pn := fields["ports"]; pn != nil {
			validatePorts(pn, name, vars, errf, warnf)
		}

		// environment
		if en := fields["environment"]; en != nil {
			validateEnvironment(en, name, vars, errf)
		}

		// volumes
		if vn := fields["volumes"]; vn != nil {
			validateVolumes(vn, name, vars, errf, warnf)
		}

		// command / entrypoint
		for _, key := range []string{"command", "entrypoint"} {
			if n := fields[key]; n != nil && n.Kind != yaml.ScalarNode && n.Kind != yaml.SequenceNode {
				errf(n.Line, n.Column, name+"."+key, "doit être une chaîne ou une liste")
			}
		}

		// labels
		if ln := fields["labels"]; ln != nil && ln.Kind != yaml.MappingNode && ln.Kind != yaml.SequenceNode {
			errf(ln.Line, ln.Column, name+".labels", "doit être un objet ou une liste")
		}
	}

	// Multiple services are a normal case (stacks, service selector): no warning.
	sort.Strings(res.Services)

	res.Valid = len(res.Errors) == 0
	return res
}

func validatePorts(n *yaml.Node, service string, vars map[string]string, errf func(int, int, string, string), warnf func(int, int, string, string)) {
	field := service + ".ports"
	items := []*yaml.Node{n}
	if n.Kind == yaml.SequenceNode {
		items = n.Content
	} else if n.Kind != yaml.ScalarNode && n.Kind != yaml.MappingNode {
		errf(n.Line, n.Column, field, "doit être une liste de ports")
		return
	}
	if len(items) == 0 {
		errf(n.Line, n.Column, field, "liste de ports vide")
		return
	}

	for _, item := range items {
		if item.Kind == yaml.MappingNode {
			// long syntax: target/published
			var target, published *yaml.Node
			for j := 0; j+1 < len(item.Content); j += 2 {
				switch item.Content[j].Value {
				case "target":
					target = item.Content[j+1]
				case "published":
					published = item.Content[j+1]
				}
			}
			if target == nil {
				errf(item.Line, item.Column, field, "syntaxe longue : clé 'target' manquante")
				continue
			}
			if published != nil {
				if _, err := strconv.Atoi(strings.TrimSpace(published.Value)); err != nil {
					errf(published.Line, published.Column, field, "port publié non numérique")
				}
			}
			continue
		}
		spec := substituteEnv(strings.TrimSpace(item.Value), vars)
		if unresolvedVarRe.MatchString(spec) {
			warnf(item.Line, item.Column, field, "variable non résolue dans le mapping de port")
			continue
		}
		if _, _, err := parsePort(spec); err != nil {
			errf(item.Line, item.Column, field, err.Error())
		}
	}
}

func validateEnvironment(n *yaml.Node, service string, vars map[string]string, errf func(int, int, string, string)) {
	field := service + ".environment"
	switch n.Kind {
	case yaml.MappingNode:
		for j := 0; j+1 < len(n.Content); j += 2 {
			key := n.Content[j]
			if !envKeyRe.MatchString(key.Value) {
				errf(key.Line, key.Column, field, fmt.Sprintf("nom de variable invalide %q", key.Value))
			}
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			val := strings.TrimSpace(substituteEnv(item.Value, vars))
			if val == "" {
				errf(item.Line, item.Column, field, "entrée vide")
				continue
			}
			key := val
			if i := strings.IndexByte(val, '='); i >= 0 {
				key = val[:i]
			}
			if !envKeyRe.MatchString(key) {
				errf(item.Line, item.Column, field, fmt.Sprintf("format attendu CLE=VALEUR, obtenu %q", val))
			}
		}
	default:
		errf(n.Line, n.Column, field, "doit être un objet ou une liste CLE=VALEUR")
	}
}

func validateVolumes(n *yaml.Node, service string, vars map[string]string, errf func(int, int, string, string), warnf func(int, int, string, string)) {
	field := service + ".volumes"
	if n.Kind != yaml.SequenceNode {
		errf(n.Line, n.Column, field, "doit être une liste")
		return
	}
	for _, item := range n.Content {
		if item.Kind == yaml.MappingNode {
			continue // long syntax handled by the engine
		}
		spec := substituteEnv(strings.TrimSpace(item.Value), vars)
		if spec == "" {
			errf(item.Line, item.Column, field, "entrée vide")
			continue
		}
		if strings.Contains(spec, " ") {
			errf(item.Line, item.Column, field, fmt.Sprintf("espace inattendu dans %q", spec))
			continue
		}
		if unresolvedVarRe.MatchString(spec) {
			warnf(item.Line, item.Column, field, "variable non résolue dans le volume")
			continue
		}
		parts := strings.Split(spec, ":")
		if len(parts) < 2 {
			warnf(item.Line, item.Column, field, fmt.Sprintf("volume anonyme %q (données non persistées sur l'hôte)", spec))
		}
	}
}

// envGaps reports the variables referenced by the compose that are missing from
// the .env (sorted) and those declared there with no value. Variables using a
// ${VAR:-default} form are optional and therefore ignored.
func envGaps(doc string, vars map[string]string) (missing, empty []string) {
	required := map[string]struct{}{}
	for _, m := range envBracedRe.FindAllStringSubmatch(doc, -1) {
		if len(m) > 2 && m[2] != "" {
			continue // ${VAR:-default}: has a fallback
		}
		required[m[1]] = struct{}{}
	}
	for _, m := range envPlainRe.FindAllStringSubmatch(doc, -1) {
		required[m[1]] = struct{}{}
	}

	for name := range required {
		val, ok := vars[name]
		switch {
		case !ok:
			missing = append(missing, name)
		case strings.TrimSpace(val) == "":
			empty = append(empty, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(empty)
	return missing, empty
}

var serviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func validServiceName(name string) bool {
	return serviceNameRe.MatchString(name)
}

// firstOddIndent returns the first non-blank line whose indentation is not a
// multiple of two spaces, along with the number of leading spaces.
func firstOddIndent(doc string) (int, int) {
	for i, line := range strings.Split(doc, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		trimmed := strings.TrimLeft(line, " ")
		spaces := len(line) - len(trimmed)
		if strings.HasPrefix(trimmed, "-") && spaces%2 == 1 {
			// Sequence dashes may be offset by one relative to their key.
			continue
		}
		if spaces%2 != 0 {
			return i + 1, spaces
		}
	}
	return 0, 0
}
