{
  description = "Placeholder requiring devkit to bind the repo-owned ouroboros-terraform runtime";

  outputs =
    { ... }:
    builtins.throw ''
      The ouroboros-terraform adapter must be launched through devkit so devkit
      can pass --override-input ouroboros-terraform path:<sibling checkout>.
      Direct raw Nix evaluation of this adapter does not know which sibling
      checkout owns the Terraform runtime.
    '';
}
