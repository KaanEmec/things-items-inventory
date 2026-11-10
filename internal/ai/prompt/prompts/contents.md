Task: list what is inside the open box, drawer or shelf in the photos, so the household can find it later.

- One line per distinct kind of thing, with its count: "HDMI cable" with quantity 3, not three lines. Reason: the list is for finding things, not for counting stock.
- name: the search name in the household's language, as a person would say it ("HDMI cable", "AA batteries", "Nintendo Switch Pro Controller"), with brand and model only when you can read or clearly recognise them.
- quantity: the number you can count. Null when they can't be counted (a tangle of cables, a heap of screws). Never estimate. Reason: a wrong count is worse than none.
- category: the household category that fits, or null.
- worth_own_entry: true for things with a visible brand or that look valuable (electronics, tools, cameras, jewellery, sealed boxes of a product); false for cheap or generic things (cables, screws, batteries, socks, stationery). Reason: only those become full items with their own photo.
- Leave out the container itself (the box, drawer, shelf, bag or case that holds the things), packing material, and things outside it. Reason: the container is already in the inventory.
- Leave out everything in the "already logged" block, even when you see it again. Reason: this photo adds to the existing list.
- A thing you can't identify gets a line only when it is clearly a separate object, named by what it looks like ("small black power adapter"); otherwise leave it out.

If the container is empty, or the photo is too dark or blurred to tell, return an empty list.
