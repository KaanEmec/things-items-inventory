# ai-eval

Runs the AI test set against a real endpoint and prints, per field, how often
the answer was right, empty or wrong, plus the structured-output mode used and
the tokens spent. Run it by hand when a prompt (`internal/ai/prompt/prompts/`)
or a model changes. It is not part of CI.

```sh
export INVENTORY_AI_VISION_TOKEN=sk-or-...        # OpenRouter by default
export INVENTORY_AI_VISION_MODEL=openai/gpt-4.1-mini
# optional: INVENTORY_AI_VISION_BASE_URL, INVENTORY_AI_TEXT_{BASE_URL,TOKEN,MODEL}
make ai-eval CASES=~/inventory-ai-cases
make ai-eval CASES=~/inventory-ai-cases EVALFLAGS="-mode strict -v -only receipt"
```

`-mode strict` checks that a model accepts every schema in strict mode (the
J6.1 acceptance test: run it once per vision model). `-v` prints each answer
and each wrong field.

## Building the test set

About 30 cases, photographed in Kaan's household. **Keep the photos outside
this repository** (for example `~/inventory-ai-cases`): they show a real home.

A good spread:

| cases | what | task |
|---|---|---|
| 6 | gadgets with a readable label or logo (headphones, charger, router, laptop) | `item_draft` |
| 4 | gadgets without a visible label, including look-alike models (phones) | `item_draft` |
| 4 | clothing, two with the care label as `<name>.label.jpg` | `item_draft` |
| 2 | electronic parts and cables | `item_draft` |
| 4 | close-ups of nameplates, rating plates, care labels | `label` |
| 4 | receipts and invoices, one with several products, one Turkish | `receipt` |
| 4 | open boxes, drawers, a shelf | `contents` |
| 2+ | typed rows, a pasted list or order email, a CSV export | `row_completion`, `text_to_items`, `csv_mapping` |

Take photos the way the app will get them: phone camera, normal light, the
thing filling most of the frame. Include a few hard ones (glare on a label, a
dark drawer): the point is to see the AI leave fields empty rather than invent.

## Case files

For a case `<name>` the directory holds:

- `<name>.jpg` (or `.png`): the photo. For `item_draft`, optional
  `<name>.label.jpg` and `<name>.receipt.jpg` are sent with it, label first.
- or `<name>.txt`: text. `row_completion` reads one typed row per line;
  `text_to_items` sends the whole text; `csv_mapping` reads it as CSV (header
  plus up to five sample rows).
- `<name>.expected.json`:

```json
{
  "task": "item_draft",
  "grounding": {"language": "tr", "currency": "TRY", "brands": ["Sony"],
                "places": [{"id": "box12", "path": "Box 12"}]},
  "item": {"name": "Sony WH-1000XM4 headphones", "category": "gadget"},
  "container": "Box 12",
  "existing": [{"name": "HDMI cable", "quantity": 3}],
  "expect": {
    "category": "gadget",
    "brand": "Sony",
    "model": "WH-1000XM4",
    "serial": null,
    "name": {"oneOf": ["Sony WH-1000XM4 headphones", "Sony WH-1000XM4 kulaklık"]},
    "attributes.connectivity": ["bluetooth", "usb_c"]
  }
}
```

- `task` defaults to `item_draft` for a photo and `text_to_items` for text.
- `grounding` is the household context; `item` is what the item is believed
  to be (for `label` and `receipt`); `container` and `existing` are for
  `contents`. Leave out what doesn't apply.
- `expect` lists only the fields you want scored, by the paths of
  `items.ai_fields`: `name`, `category`, `brand`, `model`, `serial`, `color`,
  `condition`, `quantity`, `notes`, `attributes.<key>`, `purchase_date`,
  `price_minor` (minor units: 27900 for 279.00), `currency`, `seller`, plus
  `warranty_months` and `needs` (a list of need kinds). `receipt` adds
  `item_line` and `line_count`; `label` adds `category`; `csv_mapping` uses
  `column.<i>` (`"name"`, `"attribute:size"`), `category`, `date_format`,
  `decimal_comma`.
- `null` means the field should stay empty: an answer there counts as wrong.
  That is how invented values are caught, so write `null` for every field the
  photo doesn't show (a serial on a photo without a label).
- `{"oneOf": [...]}` accepts several values. Text compares ignoring case and
  extra spaces, except `model` and `serial`, which must match exactly. Lists
  compare as sets.
- List tasks use `"rows": [...]` (`row_completion`, `text_to_items`) or
  `"lines": [...]` (`contents`), each an object of expected fields. Rows are
  matched by `id` (row number, from 1, for `row_completion`) or else by the
  most similar `name`; an expected row with no match counts its fields as
  empty, and answer rows matching nothing are reported as extra.

## Reading the result

The last line checks the J6.1 acceptance criterion: a wrong brand or model
must be rarer than an empty one. If it fails, look at the wrong fields with
`-v` and tighten the prompt (and bump its version in
`internal/ai/prompt/tasks.go`) rather than the expected answers.
