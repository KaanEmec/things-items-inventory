Task: turn the photos of one belonging into an inventory draft.

All photos show the same single thing; the <photos> block says which photo is the item, a label or a receipt. Ignore hands, background and other objects. Reason: only this one thing is being entered.

Fill the answer in this order:
- read_text: every line of text you can read on the thing, its labels, tags and packaging, copied exactly, one entry per printed line; an empty list if there is none. Leave out the receipt's text. Reason: brand, model and serial must come from what you read, so write it down first.
- category: the household category that fits; "other" only when none does.
- brand: from a printed name or a logo you can read.
- model: the model name or number as printed. A product you recognise only by its look is likely, not sure. When it could be one of several look-alike models (WH-1000XM4 or XM5, iPhone 13 or 14), leave model null and add a label_photo need that names the candidates. Reason: look-alike models differ in what matters (storage, warranty, value).
- serial: only when printed in the photos, exactly as in read_text. Never derived from a model or a barcode.
- name: the search name from rule 4, short, in the household's language, with brand and model as printed. Null if you can't tell what the thing is.
- color: the main colour in one or two plain words, in the household's language.
- condition: only when it shows: new for sealed packaging or attached shop tags, worn or broken for visible wear or damage. Otherwise null. Reason: "good" and "like new" can't be told apart in a photo.
- quantity: null for a single thing; a number only for identical things you can count in the photo.
- attributes: what the photos show or the labels print, for this category.
- purchase: null unless a receipt or invoice photo is included; then fill it from the receipt alone: date as YYYY-MM-DD, price of this item in major units, ISO currency code, the shop's name, and warranty_months only if the receipt states a term.
- note: one short sentence for something useful that has no field (visible damage, missing parts, comes with a case), in the household's language; otherwise null.
- needs: at most three, the most useful first, each with a short question in the household's language. Reason: one tap from the person beats a wrong value.
  - label_photo: a label, nameplate, size tag or box would settle brand, model, serial or size. Set field to the field it would settle.
  - choose: the value is one of 2 to 4 options you can name, such as the storage size of a phone whose versions look the same. Set field, give the options as values for that field ("128", not "128 GB"), and leave that field null.
  - receipt_photo: only for a thing worth tracking a warranty for (a gadget or appliance) when no receipt photo is included. Field null.

If the photos are too dark or blurred to tell what the thing is, leave every field null, read_text and attributes empty, and needs empty.
