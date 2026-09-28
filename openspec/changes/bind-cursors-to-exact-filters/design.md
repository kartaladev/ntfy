## Context

See `proposal.md` (Why) for the defect and its proof. Where the cursor lives today:

| Where | What |
| --- | --- |
| `store.go:363-370` | `cursorPayload{CreatedAt "t", ID "i", Filter "f"}`, JSON, then base64url (`EncodeCursor`) |
| `store.go:383-415` | `DecodeCursor` refuses a payload whose `f` differs from `q.fingerprint()`, with `/cursor: belongs to another recipient or filter set` |
| `store.go:417-433` | `ListQuery.fingerprint`: sorted states and sorted kinds, each joined with `,`; the four fields joined with NUL; SHA-256; the first 8 bytes in hex |
| `store.go:284-335` | `ListQuery.ValidateWithin` validates the recipient (required, NUL-free, UTF-8), the subject and each kind (NUL-free, UTF-8, **empty allowed**, commas allowed), then calls `DecodeCursor` |
| `memory.go:304`, `sqlstore/read.go:34` | each store's `List` calls `DecodeCursor` directly on the query it was given |
| `memory.go:354`, `sqlstore` | a kind filter matches `n.Kind` exactly. `[""]` therefore matches nothing, because `Draft` validation requires a non-empty kind |

Since PR #11, identifiers cannot hold NUL, so the NUL separator can no longer be crossed through validated input. The `,` separator can still be crossed, and so can the `[]` / `[""]` pair, where an empty join equals a join of one empty string.

## Goals / Non-Goals

**Goals**

- Two queries share a fingerprint only when their recipient, their set of states, their set of kinds and their subject are equal. This holds for any bytes, whether or not the query was validated first.
- Keep the equalities that are correct today: the order of values in a filter does not matter, and a `nil` filter is the same as an empty one.
- No change to the cursor's wire shape, the refusal's wording, or any store.

**Non-Goals**

- **Signing or encrypting cursors.** A cursor is unsigned, and its fingerprint is computed from values the client already knows, so a client can build a cursor for any filter it likes. The binding protects honest clients from continuing the wrong listing. It is not an access control: a listing is always limited to `q.Recipient`, which the service takes from the acting user. Making it an access control is a separate decision.
- **Treating duplicate kinds as one.** `["a","a"]` and `["a"]` list the same notifications, but they get different fingerprints today and still will. Refusing a cursor there is safe, if strict. Relaxing it widens what is accepted, which this fix does not need.
- **Refusing an empty kind in a list filter.** `CloseRequest` refuses one. `ListQuery` accepts one, and `?kind=` currently gives an empty page. Changing that is a validation change with its own compatibility cost, and the new encoding tells `[]` and `[""]` apart without it.
- **A conformance case in `ntfytest`.** Every store in this repository, and any host store built on `EncodeCursor`, shares the one codec that the core test covers. A host store with its own codec is outside the guarantee today, and stays outside it.

## Decisions

### D1. Length-prefixed fields, hashed as before

The fingerprint hashes a byte string built like this:
- each scalar field is written as its length (`binary.AppendUvarint`), then its bytes;
- each list is written as its count, then each value as a length-prefixed field.

The order is recipient, sorted states, sorted kinds, subject. The digest stays SHA-256, truncated to 8 bytes and written in hex, so `f` keeps its 16-character shape.

```go
func (q ListQuery) fingerprint() string {
	states := make([]string, 0, len(q.States))
	for _, state := range q.States {
		states = append(states, string(state))
	}

	slices.Sort(states)

	kinds := slices.Clone(q.Kinds)
	slices.Sort(kinds)

	var encoded []byte
	encoded = appendField(encoded, q.Recipient)
	encoded = appendFields(encoded, states)
	encoded = appendFields(encoded, kinds)
	encoded = appendField(encoded, q.Subject)

	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:8])
}
```

A length-prefixed encoding is injective. From the encoded bytes, each field's boundary is read back exactly, whatever the field holds. A `nil` list and an empty list both encode as count `0`. `[""]` encodes as count `1` followed by length `0`.

Alternatives considered:
- **JSON of a canonical struct, then hash.** This is simple and stdlib-only, but `encoding/json` replaces invalid UTF-8 with U+FFFD. `"\xff"` and `"\xfe"` would then encode the same. `DecodeCursor` is exported, and stores call it on queries they have not validated, so the encoding must not depend on validation. Rejected.
- **Escape the separators** (for example, `\` before `,` and `\`). This is correct if written with care, but it is easy to get subtly wrong, and it is harder to see at a glance that it is injective. Rejected in favour of length prefixes.
- **Refuse `,` in a kind.** This fixes one symptom, leaves `[]` / `[""]`, and turns a valid kind into a validation error for every host. Rejected.
- **Store the canonical filter itself in the cursor, not a hash.** The cursor would grow with the filter (up to `MaxFilterValues` kinds of 100 bytes each), and the recipient would be echoed back in it. Rejected: the hash is enough for an equality check.

**Default and override (library-design):** the binding is a guarantee with no override. The codec is not a port. Continuing a listing under another filter can only skip or repeat notifications, and there is no policy a consumer would want to choose there. A host that wants a different cursor scheme writes its own `Store` and codec, which the `Store` interface already allows.

### D2. No compatibility path for cursors issued before the change

Every fingerprint changes, so each cursor in flight at the upgrade is refused once, and its client restarts from the first page. A cursor lives for one paging session, so this costs at most one re-read per client.

Alternatives considered:
- **Accept the old fingerprint as well, for a while.** The old fingerprint is the ambiguous one, so accepting it keeps the defect for as long as the window lasts. It also needs a version marker in the payload, and code to remove later. Rejected.
- **A version field in the payload now.** Nothing needs it yet. The payload is JSON, so a field can be added later without breaking older decoders. Not added.

No tag has been cut, so under library-design rule 7 this is recorded in `proposal.md` rather than versioned.

## Risks / Trade-offs

- [A client caches a cursor across a deploy] → It gets one `400 validation_failed` naming `/cursor` and starts again from the first page. `docs/notifications.md` says so.
- [A truncated 64-bit hash can collide] → By chance, that takes about 2³² distinct filter sets for one recipient. A client that wants a collision can already forge a cursor directly (see Non-Goals). Not mitigated further.
- [An implementer forgets that `nil` and `[]string{}` must match] → Both encode as count `0` because `appendFields` writes `len(values)`. A row in the red table guards it: a cursor from `Kinds: nil` must be accepted under `Kinds: []string{}`.

## Migration Plan

Deploy as usual. There is no schema change and no data change. Roll back by redeploying the previous version: cursors issued by the new version are then refused once, in the same way.
