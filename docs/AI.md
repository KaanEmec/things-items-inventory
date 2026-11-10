# AI setup

AI is optional. Without it, every entry path still works: you type the details
and the app saves them. With it, photos and receipts fill in item details, and
typed or pasted lists become items.

The app uses any OpenAI-compatible chat completions endpoint. OpenRouter is the
default example below; a local server works too.

## The two endpoints

| Role | Used for | Needs |
|---|---|---|
| **Photo understanding** | Item, label and receipt photos; contents photos | A model that accepts images and returns JSON |
| **Everything else** | Quick-add completion, pasted lists, CSV column mapping | A model that returns JSON |

Each role has its own base URL, token and model. "Everything else" starts with
**Same as photo understanding** on, so one token is enough. Turn it off to send
text work to a cheaper or local model.

## Set it up in Settings

Household owners and admins set this in Settings > AI. Each role has a **Test**
button that checks the endpoint, the token and the model before you rely on it.

Base URL: the address up to `/v1`, for example `https://openrouter.ai/api/v1`.
Do not add `/chat/completions` and do not put a query or `#` in the URL.

Tokens are write-only. The app encrypts them on disk and never shows them again.

## OpenRouter with one token for both roles

1. Create an API key at openrouter.ai.
2. In Settings > AI, set base URL `https://openrouter.ai/api/v1` and paste the key
   under photo understanding.
3. Pick a model that accepts images for photo understanding. Leave
   **Same as photo understanding** on for everything else.
4. Press **Test**.

## A local or other OpenAI-compatible server

Use the same steps with your server's base URL (for example
`http://localhost:11434/v1` for Ollama, if your setup serves that path). Some
local servers accept any token; enter one if the form requires it.

For one role only: turn **Same as photo understanding** off under everything
else, then give that role its own base URL, token and model.

To refuse AI endpoints on private addresses, set `INVENTORY_AI_BLOCK_PRIVATE=1`.

## Server-wide defaults

A self-hoster can set both roles with environment variables. Households can
still change them in Settings.

    INVENTORY_AI_VISION_BASE_URL, INVENTORY_AI_VISION_TOKEN, INVENTORY_AI_VISION_MODEL
    INVENTORY_AI_TEXT_BASE_URL,   INVENTORY_AI_TEXT_TOKEN,   INVENTORY_AI_TEXT_MODEL

See `docker-compose.yml` for commented examples.

## Models that pass the Test

Not yet checked. Run `make ai-eval` (see [CONTRIBUTING.md](../CONTRIBUTING.md))
on a model, then list it here with the date.

## What is sent to the endpoint

Sent:

- **Photos**: re-encoded as JPEG with the orientation applied. The re-encoding
  drops EXIF, GPS and other metadata. Only the photo you chose goes, plus a
  label or receipt photo attached to that item.
- **Typed text**: quick-add rows, pasted lists and order emails, and CSV files
  (the header and up to five sample rows).
- **Household context** for the prompt: language and currency, the brands and
  item names used most (up to 40 each), and place names for text tasks, such as
  "Box 12" or "3rd drawer · left closet · bedroom".

Never sent:

- Access tokens, AI tokens, password hashes, sessions and invite links.
- Anything from another household.

Your endpoint provider sees what is sent. Its own retention and privacy terms
apply.

## When AI is slow or down

Saving never waits for the AI. Each entry is saved first and the AI fills it in
afterwards. The AI never overwrites a field you edited, and AI-filled fields
stay marked until you confirm them.
