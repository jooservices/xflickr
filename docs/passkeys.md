# Passkey credential storage

XFlickr stores WebAuthn credential records in `webauthn_credentials`. The
credential ID is the primary key, `user_id` identifies its owner, and deleting
the user cascades to the stored credential. The credential payload is stored
as JSONB.

This document covers storage only. XFlickr does not yet support WebAuthn
registration or login ceremonies, challenge handling, origin or relying-party
validation, sign-counter checks, or passkey endpoint feature-flag gating. The
authentication options response continues to report `passkey_enabled: false`.
