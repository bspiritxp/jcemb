#!/usr/bin/env python3
import base64
import io
import json
import sys


def fail(message):
    print(json.dumps({"error": message, "python": sys.executable}), file=sys.stderr)
    sys.exit(1)


def normalize(values):
    total = sum(float(v) * float(v) for v in values) ** 0.5
    if total == 0:
        return [float(v) for v in values]
    return [float(v) / total for v in values]


def truncate(values, dimensions):
    if dimensions and dimensions > 0:
        values = values[:dimensions]
    return normalize(values)


def load_payload():
    try:
        return json.load(sys.stdin)
    except Exception as exc:
        fail(f"invalid request JSON: {exc}")


def openclip_embed(payload):
    try:
        import open_clip
        import torch
        from PIL import Image
    except Exception as exc:
        fail(f"openclip backend requires Python packages: open_clip_torch torch pillow: {exc}")

    model_name = payload.get("model") or "ViT-B-32"
    pretrained = payload.get("pretrained") or "laion2b_s34b_b79k"
    device = payload.get("device") or "cpu"
    if device == "auto":
        device = "cuda" if torch.cuda.is_available() else "mps" if getattr(torch.backends, "mps", None) and torch.backends.mps.is_available() else "cpu"

    try:
        model, _, preprocess = open_clip.create_model_and_transforms(model_name, pretrained=pretrained, device=device)
        tokenizer = open_clip.get_tokenizer(model_name)
        model.eval()
        with torch.no_grad():
            if payload.get("kind") == "image":
                raw = base64.b64decode(payload.get("content_b64") or "")
                image = Image.open(io.BytesIO(raw)).convert("RGB")
                tensor = preprocess(image).unsqueeze(0).to(device)
                vector = model.encode_image(tensor)
            else:
                tokens = tokenizer([payload.get("text") or ""]).to(device)
                vector = model.encode_text(tokens)
            vector = vector[0].detach().float().cpu().tolist()
    except Exception as exc:
        fail(f"openclip embedding failed: {exc}")

    return truncate(vector, int(payload.get("dimensions") or 0))


def jina_embed(payload):
    try:
        import torch
        from PIL import Image
        from transformers import AutoModel
    except Exception as exc:
        fail(f"jina-clip backend requires Python packages: transformers torch pillow einops timm: {exc}")

    model_name = payload.get("model") or "jinaai/jina-clip-v2"
    device = payload.get("device") or "cpu"
    if device == "auto":
        device = "cuda" if torch.cuda.is_available() else "mps" if getattr(torch.backends, "mps", None) and torch.backends.mps.is_available() else "cpu"

    try:
        model = AutoModel.from_pretrained(model_name, trust_remote_code=True).to(device)
        model.eval()
        with torch.no_grad():
            if payload.get("kind") == "image":
                raw = base64.b64decode(payload.get("content_b64") or "")
                image = Image.open(io.BytesIO(raw)).convert("RGB")
                if hasattr(model, "encode_image"):
                    vector = model.encode_image([image])
                else:
                    vector = model.encode([image])
            else:
                text = payload.get("text") or ""
                if hasattr(model, "encode_text"):
                    vector = model.encode_text([text])
                else:
                    vector = model.encode([text])
            if hasattr(vector, "detach"):
                vector = vector[0].detach().float().cpu().tolist()
            else:
                vector = vector[0].tolist()
    except Exception as exc:
        fail(f"jina-clip embedding failed: {exc}")

    return truncate(vector, int(payload.get("dimensions") or 0))


def main():
    payload = load_payload()
    provider = (payload.get("provider") or "openclip").strip().lower()
    if provider == "openclip":
        vector = openclip_embed(payload)
    elif provider in ("jina-clip", "jina"):
        vector = jina_embed(payload)
    else:
        fail(f"unsupported image provider: {provider}")
    print(json.dumps({"vector": vector}, separators=(",", ":")))


if __name__ == "__main__":
    main()
