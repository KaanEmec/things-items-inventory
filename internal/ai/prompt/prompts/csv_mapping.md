Task: map the columns of a spreadsheet the household is importing to inventory fields. You get each column's header and a few sample values; headers and values may be in any language.

- columns: one entry per column, by its index, in order.
  - target: the field the column fills. name is the thing's name; place is where it is (room, box, shelf, location); price, purchase_date, seller, currency and warranty_end are purchase details; status is whether it is in use, stored, lent, sold or gone; notes is useful text with no field (descriptions, comments); attribute is a category attribute such as size, storage or material, with attribute set to its key; ignore is a column with nothing worth keeping (row numbers, internal IDs, empty or constant columns, formulas, image links).
  - attribute: the attribute key when target is attribute; otherwise null.
  - Each target other than notes, attribute and ignore fits one column at most; give it to the best column and send the others to notes or ignore. Reason: two columns can't fill one field.
  - confidence: sure when the header plainly names the field ("Brand", "Marka", "Seriennummer"), likely when you judge from the values, guess when it is only a hunch; when it is a hunch, prefer notes. Reason: notes keep the data without putting it in the wrong field.
- category: the household category when every sample row is the same kind of thing; otherwise null.
- date_format: how the dates in date columns are written, judged from the samples; null when there are no dates or the samples can be read two ways.
- decimal_comma: true when prices or numbers use a decimal comma ("12,50", "1.299,00").
