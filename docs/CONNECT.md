# Reaching your Ghost when you are away

A decision record. It says what we ship, what we charge for, and what must be
true before anyone is charged.

## The problem

On your home network the phone talks straight to the Pod. Away from home it
cannot: the Pod sits behind a router with no public address. Tailscale solves
this today, but it asks a normal person to make an account, install a second
app and understand a VPN. That is fine for us and wrong for the people we want.

## Options

| Option | Who it suits | Cost to the owner | Verdict |
|---|---|---|---|
| Home network only | Everyone, at home | Free | Always works, always free |
| Tailscale | Technical owners | Free | Keep as the documented advanced path |
| Self-hosted relay (`ghost-relay-server`) | Owners with a server | Their own server | Keep open source and supported |
| **Ghost Connect**, a hosted relay | Everyone else | Paid | Build it, with the rules below |
| Port forwarding / dynamic DNS | Nobody who values their Pod | Free | Never recommend: it exposes the Pod to the internet |

## Decision

Build **Ghost Connect**: zero-setup remote access, sold as a small subscription.
It is a convenience, not a gate.

1. **Local use and self-hosting stay free forever.** The promise is that the
   Pod is yours. A feature that stops working when a subscription lapses would
   break it. If Ghost Connect lapses, the Pod keeps working on the home network,
   over Tailscale, and through a self-hosted relay.
2. **What the subscription buys:** remote access with no router or VPN setup, a
   stable address, and support. It is priced to cover a small always-on relay
   and to be easy to say yes to (around US$4-6 a month, or a year included with
   a Pod). The cost per Pod is a single idle connection.
3. **Push notifications do not need Ghost Connect.** The Pod sends them straight
   to Expo's push service, so reminders and alerts reach a phone with the app
   closed on the free tier as well.
4. **Payment is a separate thing from data.** The site holds an email and a
   billing record. It sees no conversation, memory or file. A subscription
   produces a signed entitlement (an Ed25519 token) that the Pod shows to the
   relay; the relay checks the signature and nothing else about the owner.

## What must be true before anyone is charged

A hosted relay is only acceptable if the company running it cannot read or act
on an owner's Ghost.

| Requirement | State |
|---|---|
| Relayed requests must present device credentials, exactly like any remote peer. | **Done (v0.24.90).** |
| End-to-end encryption between phone and Pod, so the relay carries ciphertext only. The Pod's public key travels in the pairing link and the phone pins it. | **Done.** The relay carries a sealed exchange on `/v1/sealed` and cannot see the path, the credentials, the request or the reply. The Pod opens it (`pkg/relayclient/sealed.go`), enforces the client's scope itself, refuses replays and stale requests, and a Pod linked to Ghost Connect refuses plain requests outright. The phone side is `lib/sealedFetch.ts` in ghost-app, pinned byte for byte to `pkg/relaycrypto` by a shared test vector. Not yet covered: file uploads and the live WebSocket, which still need the home network. |
| The relay has no way to approve, revoke, download or otherwise act. | Follows from the two above. |
| Only a paid Pod may use the hosted relay. | **Done.** The relay verifies an Ed25519 entitlement signed by the site (`pkg/entitlement`), for exactly that Pod, and closes the tunnel when it expires. A Pod enrolls itself with its entitlement, so the relay never talks to the site. |
| Per-device relay tokens; no shared secret; instant revoke. | Exists (`ghost relay revoke`). Needs an audit before launch. |
| Rate limits, abuse handling, and logs that keep connection times and byte counts but never content. | **Done for limits and logs** (enroll, connect and request limits; per-tunnel byte counts; no payload in any log line). Abuse handling is still by hand. |
| A public security write-up and an independent review of the relay and the pairing. | The write-up is the site's Security page. The independent review is still **before launch**. |

## How a Pod joins Ghost Connect

```
Pod                       Site (ghost-site)                 Relay
 | ghost relay link          |                                |
 |-- POST /api/connect/start |  pod id                        |
 |<- code, poll token -------|                                |
 |   (owner signs in on the site, types the code, has a plan) |
 |-- POST /api/connect/poll->|                                |
 |<- pass (Ed25519, 3 days) -|                                |
 |-- POST /v1/enroll {pass, device secret} ------------------>|  verifies the pass for this Pod
 |== tunnel, header X-Ghost-Entitlement: pass ===============>|
 |-- renew daily: POST /api/connect/renew (Bearer pass) ----->|  (site)   OpEntitlement (relay)
```

The pass is `ge1.<payload>.<signature>`; `pkg/entitlement/contract_test.go` and
the site's `tests/entitlement.test.ts` pin the same token. The relay is told the
site's public key with `--entitlement-keys` (several may be listed, so the
signing key can be rotated). With no key it is an ordinary self-hosted relay.

## Order of work

1. ~~End-to-end encryption in the relay tunnel.~~ Done.
2. ~~Relay hardening: rate limits, content-free logs.~~ Done. Token audit remains.
3. ~~The site: sign-up, Stripe Checkout, the entitlement, a status page.~~ Built
   (`ghost-site`), awaiting a domain, Stripe keys and a deploy.
4. ~~Onboarding: after paying, link with a code.~~ Done (`ghost relay link`).
5. Independent review of the relay and the pairing.
6. Only then set a price and announce it.

## For now

Keep Tailscale for yourself. A normal owner at home needs nothing. Do not open
Ghost Connect widely before step 5: charging people for a relay nobody
independent has looked at would be early.
