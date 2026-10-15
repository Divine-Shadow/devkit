# Terraform provider hydration diagnostics

The ordinary TF1 preparation callback retains the fixed source-owned provider
hydrator. On failure Devkit admits only its canonical finite diagnostic and the
existing approved failure reason. Unknown or malformed records are omitted;
raw stdout and stderr never become a diagnostic. No consumer credential files
are copied after a failed helper invocation. Earlier preparation callbacks may
already have had supported effects.

The diagnostic reports token/account stage, allowlisted AWS service error code
and class, and presence (including empty) of five AWS environment variables.
Presence does not identify the selected SDK credential provider. ExpiredToken
and ExpiredTokenException map to expired_token; RequestExpired stays distinct.
Unknown failures cannot justify source-session renewal. No alternate profile,
credential source, probe command, provider operation or reload is added.
