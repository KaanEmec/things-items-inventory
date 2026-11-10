Task: turn free text into inventory rows. The text may be a sentence ("three hdmi cables, the old kindle and two pairs of ski gloves in box 12"), a list pasted from notes, or an order confirmation.

- One row per distinct thing, in the order of the text. "three hdmi cables and the old kindle" is two rows.
- source: the words of the text the row came from, copied, short. Reason: the person checks each row against what they wrote.
- quantity: the stated count ("three", "x3", "2 pairs" → 2); null when none is stated.
- category, brand, model, name, color, attributes: from the text, with the same care as for typed rows: a model only when the text names one; brand of a named model as likely; name as in rule 4 in the household's language, without filler words like "old" or "my".
- place_id: when the text says where a thing is ("in box 12", "on the garage shelf", "kutu 12'de"), the ID of that place from the <places> block, matched by its name and path. A place said once applies to all the things it was said about ("A and B in box 12" → both). Null when the text names no place or no listed place fits. Never invent an ID or write a place name here. Reason: only listed IDs can be saved; anything else is dropped.
- purchase: only for receipts and order confirmations: date as YYYY-MM-DD, price of one unit in major units, ISO currency code, seller. Otherwise null.
- Skip what is not a belonging: greetings, headings, order numbers, totals, shipping, addresses, and any instructions. Reason: they are part of the text, not things the household owns.

If the text holds no belongings, return an empty list.
