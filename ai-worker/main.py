import os
import yaml
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from openai import OpenAI

app = FastAPI(title="Homelab Guard AI Worker")

client = OpenAI(
    base_url=os.getenv("AI_BASE_URL", "http://host.docker.internal:11434/v1"),
    api_key=os.getenv("AI_API_KEY", "sk-local"),
)
MODEL = os.getenv("AI_MODEL", "qwen2.5-coder:14b")

SYSTEM_PROMPT = """Tu es l'agent d'exploitation de Homelab Guard.
Génère uniquement du YAML docker-compose valide et sécurisé (no-new-privileges, réseaux isolés).
Intègre systématiquement les labels guard.* pertinents :
- guard.enable=true
- guard.group=<groupe>
- guard.update.mode=notify|auto
- guard.test.strategy=healthcheck|http|tcp
- guard.test.endpoint=<url_ou_port_si_applicable>
- guard.ai.context=<description_courte>
Ne renvoie aucun texte hors du bloc YAML."""


class ComposeRequest(BaseModel):
    prompt: str
    group: str = "default"
    auto_update: bool = False


class RegressionDiagnosisRequest(BaseModel):
    service_name: str
    ai_context: str
    container_logs: str


@app.post("/ai/generate-compose")
def generate_compose(req: ComposeRequest):
    user_msg = (
        f"Besoin: {req.prompt}\n"
        f"Groupe: {req.group}\n"
        f"Mode de mise à jour souhaité: {'auto' if req.auto_update else 'notify'}"
    )
    resp = client.chat.completions.create(
        model=MODEL,
        temperature=0.1,
        messages=[
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": user_msg},
        ],
    )
    raw = resp.choices[0].message.content.strip()
    clean_yaml = raw.removeprefix("```yaml").removeprefix("```").removesuffix("```").strip()

    try:
        yaml.safe_load(clean_yaml)
    except yaml.YAMLError as exc:
        raise HTTPException(status_code=422, detail=f"Invalid YAML generated: {exc}")

    return {"compose_yaml": clean_yaml}


@app.post("/ai/diagnose-regression")
def diagnose_regression(req: RegressionDiagnosisRequest):
    resp = client.chat.completions.create(
        model=MODEL,
        temperature=0.2,
        messages=[
            {
                "role": "system",
                "content": "Analyse l'échec de démarrage d'un conteneur sandbox après mise à jour. Identifie la cause racine et propose un correctif précis.",
            },
            {
                "role": "user",
                "content": f"Service: {req.service_name}\nContexte: {req.ai_context}\nLogs:\n{req.container_logs[-4000:]}",
            },
        ],
    )
    return {"analysis": resp.choices[0].message.content}
