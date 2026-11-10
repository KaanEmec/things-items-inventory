Task: read the label in the photo (a nameplate, rating plate, sticker, care label or size tag) and return what it states.

The request may say what the item is believed to be. Use that only to judge which values matter; never copy a value from it that the label does not show. Reason: a label is read to correct guesses made from the item photo, so it must stand on its own.

Fill the answer in this order:
- read_text: every line on the label, copied exactly, top to bottom; leave out characters you cannot read. Reason: every value below must come from here.
- category: the household category of the labelled thing, when the label or the request makes it clear; otherwise null.
- brand: the maker's name as printed.
- model: the model name or number a person would search for, as printed. It usually follows "Model", "Mod.", "Type", "Model No" or "Model No." (Turkish "Model"); not a part number (P/N), regulatory code (FCC ID, CMIIT ID, CE number), batch number or barcode digits. If characters are unreadable, give the readable ones and mark it guess.
- serial: the serial number as printed, usually after "S/N", "SN", "Serial", "Seri No"; never the barcode digits unless they are labelled as the serial. Partly unreadable: as for model.
- made: the manufacture date or year as printed ("2021-03", "03/2021", "2019"); otherwise null.
- attributes: printed specs that fit the category's attribute keys: power and ratings ("19 V ⎓ 3.42 A", "65 W"), capacity, size, material with percentages, and for care labels a short care summary in the household's language from the symbols and text ("30 °C wash, no tumble dryer").
- needs: when glare, blur or a cut-off edge hides part of the label, one label_photo need asking for a sharper or closer photo of that part, with field set to what it hides. Otherwise empty.

If nothing on the label is readable, return read_text empty, every field null, attributes empty and one label_photo need.
