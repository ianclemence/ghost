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
on an owner's Ghost. Two of these are not yet true.

| Requirement | State |
|---|---|
| Relayed requests must present device credentials, exactly like any remote peer. | **Done (v0.24.90).** Before this the relay replayed traffic on the Pod's own loopback, which the gateway treats as the owner with no credentials. A hosted relay would have held owner-level access to every customer's Pod. |
| End-to-end encryption between phone and Pod, so the relay carries ciphertext only. The Pod's public key travels in the pairing QR and the phone pins it. | **Not done. This is the gate.** The sealed envelope is built and tested (`pkg/relaycrypto`: pinned X25519 key, per-connection AES-256-GCM keys in each direction, counters that refuse replay, reordering and reflection). It is not yet wired into the relay tunnel or the app, so the relay still sees plain requests and responses inside its own TLS hop. |
| The relay has no way to approve, revoke, download or otherwise act. | Follows from the first two. |
| Per-device relay tokens; no shared secret; instant revoke. | Exists (`ghost relay revoke`). Needs an audit before launch. |
| Rate limits, abuse handling, and logs that keep connection times and byte counts but never content. | Not built. |
| A public security write-up and an independent review of the relay and the pairing. | Before launch. |

## Order of work

1. End-to-end encryption in the relay tunnel (pairing already exchanges a
   secret; add the Pod's public key and pin it in the app).
2. Relay hardening: per-device tokens, rate limits, content-free logs.
3. The site: sign-up, Stripe Checkout, the entitlement token, a status page.
4. Onboarding: after paying, the app connects with no steps. The Pod claims its
   entitlement by showing the owner a code once.
5. Only then set a price and announce it.

## For now

Keep Tailscale for yourself. A normal owner at home needs nothing. Do not launch
Ghost Connect before step 1: charging people for a relay that can read their
messages would contradict what Ghost is.
