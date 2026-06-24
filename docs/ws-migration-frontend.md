# Chat service WS migration — frontend handover

## What changed

The chat service now exposes a **per-user WebSocket** that auto-subscribes to every chat the user is a member of. The old per-chat socket is deprecated but still up during transition.

## Endpoints

| Old (deprecated) | New |
|---|---|
| `GET /api/chats/{chatID}/ws?token=<jwt>` | `GET /api/ws?token=<jwt>` |

- One socket per logged-in user (multi-tab/multi-device supported — open as many as you want).
- JWT is read from the **query string**, same as before.
- Use `wss://` in QA/prod, `ws://` locally.

## Wire format (server → client)

All server frames are now uniform:

```json
{
  "type": "<event-type>",
  "chat_id": "<uuid>",
  "payload": { ... }
}
```

Clients dispatch on `type` and filter on `chat_id`. New event types ride the same socket without protocol changes.

## Event catalogue

### `ready`
Sent once, right after connect. Tells you which chats you're subscribed to. Use this as the signal to call `GET /api/chats/unread` to seed badge state.
```json
{ "type": "ready", "payload": { "chat_ids": ["<uuid>", "..."] } }
```

### `message.created`
Fires for every new message in any subscribed chat — including chats the user isn't currently viewing. **This is what lights up unread badges live.**
```json
{
  "type": "message.created",
  "chat_id": "<uuid>",
  "payload": {
    "id": "<uuid>",
    "sender_id": "<uuid>",
    "content": "...",
    "message_type": "text",
    "created_at": "<iso8601>"
  }
}
```

### `message.deleted`
```json
{
  "type": "message.deleted",
  "chat_id": "<uuid>",
  "payload": { "id": "<uuid>", "deleted_at": "<iso8601>" }
}
```

### `chat.read`
Fires when **any** member of a chat marks it read — including the same user's other devices. Use this to clear the badge across devices.
```json
{
  "type": "chat.read",
  "chat_id": "<uuid>",
  "payload": { "user_id": "<uuid>", "last_read_message_id": "<uuid>" }
}
```

### `error`
```json
{ "type": "error", "payload": { "error": "..." } }
```

## Wire format (client → server)

The only inbound actions still supported are message ops. **Drop `subscribe`/`unsubscribe`** — subscriptions are now server-driven from membership.

```json
{ "action": "send_message",   "chat_id": "<uuid>", "content": "..." }
{ "action": "delete_message", "chat_id": "<uuid>", "message_id": "<uuid>" }
```

## Reconnection contract

The server may close the socket if the outbound buffer overflows (slow client, bad network). On any disconnect:

1. Reconnect to `/api/ws?token=...`.
2. Wait for the `ready` frame.
3. Call `GET /api/chats/unread` to re-sync badge counts.

There is **no event replay**. The HTTP re-sync is the source of truth after reconnect.

## What stays the same

- All HTTP endpoints (`/api/chats/*`, `/api/chats/reads`, `/api/chats/unread`). No changes.
- JWT auth — same token, same place (query string for WS, `Authorization` header for HTTP).

## Known limitation

The membership lookup that drives auto-subscribe is currently inferred from `chat_reads` + sent messages. A user added to a chat who has never read or posted in it won't appear subscribed until they do. This will be replaced by an authoritative external membership API in a follow-up — no client change required when that lands.

## Migration order suggested

1. Add the new `/api/ws` connection alongside the old per-chat sockets.
2. Wire `message.created` into the unread badge.
3. Wire `chat.read` into "clear badge across devices."
4. Remove the per-chat WS code paths.
5. Backend removes the deprecated `/api/chats/{chatID}/ws` endpoint.
