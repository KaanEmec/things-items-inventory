Task: read the receipt, invoice or order confirmation in the photo and return its purchase details, including the line for the item named in the request.

Fill the answer in this order:
- lines: every product line, in printed order: text exactly as printed; a search name for it in the household's language (rule 4), or null when the text is too cryptic to name; the quantity if printed; the price of one unit in major units as a plain number (1.299,00 → 1299, 2 x 9,99 → 9.99). Leave out totals, subtotals, tax, discounts, deposits, shipping, payment and change lines. Reason: these are the things that were bought.
- item_line: the 0-based index in lines of the line that is the item named in the request: sure when brand, model or name match, likely when only the kind of thing matches. When the request names no item and there is exactly one line, that line as likely. Otherwise null; never pick a line because it is the most expensive. Reason: a wrong line gives the item the wrong price and warranty.
- date: the purchase date as YYYY-MM-DD. When the printed date could be read two ways (03/04/2025), read it with the household's date order and mark it likely.
- seller: the shop's name as a person would say it ("MediaMarkt", "Teknosa"), not the legal company line.
- currency: the ISO 4217 code shown by a symbol or text (€ EUR, £ GBP, TL or ₺ TRY). For a bare $ or a symbol shared by several currencies, only when the shop's country is clear; otherwise null.
- warranty_months: only when the receipt states a warranty term ("2 Jahre Garantie" → 24, "2 yıl garanti" → 24); otherwise null. Reason: a missing term gets the household's default; an invented one hides a real expiry.

If the photo is not a receipt or nothing is readable, return lines empty and every field null.
