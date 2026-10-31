# Current-source repository identity and acquisition endpoint

## problem

The first actual guarded Writer preparation with qualified helper g5d reached
the retained Local2 consumer and refused before acquisition: `source origin
differs from immutable declaration` (exec-e491b805-c883-4460-b9ba-3a62b45e10e7,
exit 2). The writer's raw and effective origin were both the historical
`git@github.com:Divine-Shadow/ouroboros-ide.git`. Published Devkit231 and the
actual WSL673/Devkit870 constructor75nf contain byte-identical project metadata
declaring `ssh://git@ssh.github.com:443/Divine-Shadow/ouroboros-ide.git`.
The existing origin comparison is literal; its refusal is correct under that
contract. Helper binding does not replace the project origin. Neither the GUI
projection, startup profile nor registration v1 supplies an alternate origin.

## options

Changing constructors or supplying the current registration projection cannot
change this relationship. Rewriting the writer's protected origin would mutate
its existing state. Reusing historical manifest-shrink equivalence implicitly
would exceed that migration-only contract. Instead, explicitly extend only
current-source admission to the exact historical SCP representation of the
same immutable repository, while retaining its canonical acquisition endpoint.

## selection_rationale

Root adopted this distinct source-contract increment after reading the complete
read-only proposal (libfile_354c49c283f88191af7daeab45072df7 v1, 8066 bytes).
It is not covered by the prior seven-file helper acceptance. Follow the
[Tradeoff Decision Framework](https://github.com/Divine-Shadow/dev/blob/main/docs/decision-framework/tradeoff_decision_framework.md): explicit
repository identity and endpoint contracts first, consequence tests second,
state preservation and truthful receiving claims before delivery speed.

Keep exact-origin admission. The sole additional representation is
`git@github.com:<owner>/ouroboros-ide.git` when the immutable declaration is
exactly `ssh://git@ssh.github.com:443/<owner>/ouroboros-ide.git`; the safe owner
segment is derived from that declaration and compared case-sensitively.
Do not accept other repositories, users, hosts, ports, URI forms, encoded
paths, case folding, caller aliases or runtime policy selectors. Read one raw
origin without whitespace normalization, require effective origin to equal it,
and retain refusal of every repository URL rewrite. Both ls-remote and fetch
continue using the immutable declaration, never the observed SCP spelling.

## safety_checks

Retain all geometry, exact lane ownership-marker, selected helper/transport,
registration, ancestry, expected-old CAS, symbolic-ref and unknown-commit laws.
No origin/configuration, environment, trust, credential, home, candidate,
manifest or history repair is performed. Test the exact positive pair and
adversarial owner/repository/user/host/port/encoding/case/whitespace/ambiguity
and rewrite cases. Real Git through a test-owned SSH endpoint must prove that
both remote operations use only the canonical host, port and repository while
HEAD/index/candidate bytes and modes/config/FETCH_HEAD/other refs remain exact.
Independent review of the complete candidate precedes full normal Devkit CI,
publication and source-derived composition. Runtime receiving is separate.

## rollback_plan

If the relation admits any additional identity or acquisition uses the observed
alias, publish a reviewed source revert through the normal CI/composition path.
Do not rewrite consumer origins or roll back live refs locally.

## decision_scope

The existing sole Source1 Devkit writer implements this bounded contract and
its necessary tests/docs. Do not mutate qualified WSL3853. Runtime owns target
selection, delivery, changed-consumer receiving and op6a55 disposition; Writer
retains the preserved EXPOSE candidate. No unchanged preparation retry, live
lifecycle effect, new Product registration service or fresh human grant follows.
