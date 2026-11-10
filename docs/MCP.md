# Agent guide: access tokens and MCP

Agents (AI assistants with MCP support, or your own scripts) can read and
change the inventory through an access token. Each token belongs to one
household and acts with its owner's role.

## Create an access token

1. In the app, open Settings and create an access token. Give it a name
   that says what it is for, such as "Claude desktop".
2. Choose **read** (looks only) or **write** (can change items).
3. Copy the token when it is shown. The app keeps only a hash, so it cannot show
   it again. Tokens start with `stash_pat_`.
4. To revoke a token, delete it in the same list. It stops working at once.

Treat a token like a password. Anyone with it can act in your household.

## What agents can do

- A **read** token gets the read tools only.
- A **write** token also gets the write tools. No tool deletes anything or
  changes settings, members, AI profiles or tokens.
- Items an agent adds are **drafts**. They land in the Drafts queue for a person
  to confirm.
- Changes show in the item history as made "via" the token's name.

## Connect an MCP client

The endpoint is `POST /mcp` on your public address, for example
`https://stuff.example.com/mcp`. Send the token in the header:

    Authorization: Bearer stash_pat_...

Example client configuration (Claude Code, Claude Desktop and most clients
take this shape; Settings, Access tokens shows it with your address):

    {"mcpServers": {"inventory": {"type": "http",
      "url": "https://stuff.example.com/mcp",
      "headers": {"Authorization": "Bearer stash_pat_..."}}}}

Tools: `search`, `get_item`, `get_place`, `get_box`, `list_items`,
`list_places`, `get_history` (read); `add_item`, `update_item`, `move_item`,
`set_status`, `add_contents_lines`, `create_place`, `create_box` (write).

## Plain HTTP API

Scripts can call the REST API under `/api/v1` with the same header. The routes
are listed in the API reference, once it is written (J9.2).
