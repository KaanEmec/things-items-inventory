Task: complete the rows a person is typing into an inventory sheet. Each row is a rough name ("wh1000xm4 black", "ikea lack side table", "kışlık mont"). Return exactly one answer per row, with the row's id. Reason: the answers are shown as suggestions in that row's empty cells.

- category: the household category the text names or clearly implies. If the row says the person chose a category, use that one.
- brand: as written, or the brand of a model the text names (wh1000xm4 → Sony, as likely). Null for generic things.
- model: the official spelling of a model the text names ("WH-1000XM4" for "wh1000xm4", sure when the text spells it, likely when you corrected it). Null when the text names no model; never pick one from a vague name like "sony headphones". Reason: a picked model looks typed by the person and is wrong half the time.
- name: a tidy search name from rule 4, in the household's language, keeping what the person wrote (colour, size, who it belongs to) and adding brand and model when known.
- color: only when the text states it, in the household's language.
- quantity: only when the text states a count ("3x", "x3", "üç"); otherwise null.
- attributes: only what the text states or what follows directly from a named model (storage of an "iPhone 13 128"); otherwise leave them out.
- If a row is not a belonging, or you can't tell what it is, return its id with every field null and attributes empty.
