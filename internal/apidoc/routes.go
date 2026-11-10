package apidoc

// routeTable describes every /api/v1 route, one per line:
//
//	METHOD /path | group | S or I | P (paged list) or - | summary
//
// S = stable for outside use (access tokens allowed). I = internal to the
// app (session cookie only, may change without notice). The route-coverage
// test in internal/server fails when a registered route is missing here or a
// line here is no longer registered.
const routeTable = `
GET /setup | App | I | - | Whether first-run setup is still needed.
POST /setup | App | I | - | First-run setup: owner, household and starter places.
POST /auth/login | App | I | - | Sign in with e-mail and password (session cookie).
POST /auth/logout | App | I | - | End this session.
POST /auth/logout-all | App | I | - | End all of the user's sessions.
POST /auth/reset | App | I | - | Set a new password from a one-time reset link.
GET /me | App | I | - | The signed-in user, households and packing box.
PATCH /me | App | I | - | Change name or e-mail.
POST /me/password | App | I | - | Change the password.
PUT /me/household | App | I | - | Switch the active household.
PUT /me/packing-box | App | I | - | Choose the box being packed.
GET /me/recent-places | App | I | - | Places this person used last.
GET /household | App | I | - | Household settings.
PATCH /household | App | I | - | Change household settings.
DELETE /household | App | I | - | Delete the household and all its data.
GET /members | App | I | - | Members and open invites.
PATCH /members/{user_id} | App | I | - | Change a member's role.
DELETE /members/{user_id} | App | I | - | Remove a member.
POST /members/{user_id}/reset-link | App | I | - | Make a password reset link for a member.
POST /invites | App | I | - | Invite someone.
DELETE /invites/{id} | App | I | - | Revoke an invite.
GET /invites/{token} | App | I | - | Preview an invite link.
POST /invites/{token}/accept | App | I | - | Accept an invite.
GET /tokens | App | I | - | The caller's own access tokens.
POST /tokens | App | I | - | Create an access token (the value is shown once).
DELETE /tokens/{id} | App | I | - | Revoke an access token.
GET /categories | Reference | S | - | Categories with their attributes, statuses, conditions and place kinds.
GET /homes | Places | S | - | List homes.
POST /homes | Places | S | - | Create a home (admin).
PATCH /homes/{id} | Places | S | - | Rename, reorder or archive a home (admin).
GET /homes/{id}/tree | Places | S | - | All places of a home, flat with parent_id and counts.
GET /places/{id} | Places | S | - | A place with its children and counts.
POST /places | Places | S | - | Create a place (kind room, furniture, spot, floor).
PATCH /places/{id} | Places | S | - | Rename, move or change a place.
DELETE /places/{id} | Places | S | - | Soft-delete a place; a non-empty one needs ?move_to=.
POST /places/{id}/restore | Places | S | - | Undo a place delete.
GET /places/{id}/lines | Contents lines | S | - | Contents lines of a place or box.
POST /places/{id}/lines/confirm | Contents lines | S | - | Mark every AI-made line of the place as confirmed.
POST /places/{id}/lines/promote-all | Contents lines | S | - | Turn every line of the place into a draft item.
POST /places/{id}/contents/read | Contents lines | I | - | Ask the AI to list a place's contents from its photo.
GET /places/{id}/history | History | S | P | Events of a place or box, newest first.
GET /items | Items | S | P | List items (filters: category, status, place_id+below, draft, warranty, sort).
POST /items | Items | S | - | Create an item; an optional client id makes retries safe.
POST /items/bulk | Items | S | - | Change place or status of several items.
GET /items/{id} | Items | S | - | One item.
PATCH /items/{id} | Items | S | - | Change some fields of an item.
DELETE /items/{id} | Items | S | - | Soft-delete an item.
POST /items/{id}/restore | Items | S | - | Undo an item delete.
POST /items/{id}/move | Items | S | - | Move an item to a place (null = not found).
POST /items/{id}/seen | Items | S | - | Record "I saw it here".
POST /items/{id}/confirm | Items | S | - | Accept the AI fields and clear the draft flag.
POST /items/{id}/ai | Items | I | - | Run the AI reader on an item again.
POST /items/{id}/label/read | Items | I | - | Read a label photo with the AI.
POST /items/{id}/receipt/read | Items | I | - | Read a receipt photo with the AI.
POST /items/ai/read-waiting | Items | I | - | Queue the AI for every photo-only draft.
GET /items/{id}/history | History | S | P | Events of an item, newest first.
POST /lines | Contents lines | S | - | Add contents lines to a place or box.
PATCH /lines/{id} | Contents lines | S | - | Change a line.
DELETE /lines/{id} | Contents lines | S | - | Soft-delete a line.
POST /lines/{id}/restore | Contents lines | S | - | Undo a line delete.
POST /lines/{id}/promote | Contents lines | S | - | Turn a line into a draft item.
POST /photos | Photos | S | - | Upload a photo (multipart) and link it to an item or place.
GET /photos/{id}/{size} | Photos | S | - | A photo file; size is orig, thumb, screen or ai.
POST /photo-links | Photos | S | - | Link an existing photo to an item or place.
PATCH /photo-links/{id} | Photos | S | - | Change a link's order or role.
DELETE /photo-links/{id} | Photos | S | - | Unlink a photo.
GET /boxes | Boxes | S | P | List boxes by number (filters: status, goes_to_id).
POST /boxes | Boxes | S | - | Create one or more numbered boxes.
GET /boxes/{id} | Boxes | S | - | A box with its items and lines.
GET /boxes/number/{n} | Boxes | S | - | The same, by the number on the box.
PATCH /boxes/{id} | Boxes | S | - | Change destination, handling, note or status.
POST /boxes/{id}/close | Boxes | S | - | Mark the box packed.
POST /boxes/bulk | Boxes | S | - | Change destination, handling or status of several boxes.
POST /boxes/{id}/unpack | Boxes | S | - | Move a box's contents to places and mark it unpacked.
GET /history | History | S | P | The household's event feed, newest first.
GET /search | Search | S | - | Search items, lines, places and boxes (q, kinds, limit).
GET /search/similar | Search | S | - | Possible duplicates by name, brand and model.
GET /overview | App | I | - | Counts and lists for the home screen.
GET /stream | App | I | - | Server-sent events for live updates.
GET /jobs/{id} | App | I | - | A background job.
GET /export | App | I | - | Export the household's data.
GET /settings/ai | App | I | - | The AI settings, without tokens.
PUT /settings/ai/{role} | App | I | - | Change an AI role (admin).
DELETE /settings/ai/{role} | App | I | - | Clear an AI role (admin).
POST /settings/ai/{role}/test | App | I | - | Test an AI role's endpoint.
POST /settings/ai/{role}/models | App | I | - | List the endpoint's models.
GET /settings/ai/activity | App | I | - | Recent AI calls.
GET /settings/ai/usage | App | I | - | AI usage totals.
POST /ai/complete-rows | App | I | - | Complete quick-add rows with the AI.
POST /ai/text-to-items | App | I | - | Turn pasted text into item rows with the AI (nothing saved).
POST /imports/preview | Import | I | - | Read a CSV or TSV file: delimiter, header, samples and a proposed column mapping.
POST /imports/dry-run | Import | I | - | Show what importing the file with a mapping would add, skip and leave unmatched.
POST /imports | Import | I | - | Import the file as draft items in a background job.
POST /imports/{id}/undo | Import | I | - | Remove exactly the items an import added.
`
