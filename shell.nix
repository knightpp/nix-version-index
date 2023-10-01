let
  pkgs = import <nixpkgs> {};
in
  pkgs.mkShell
  {
    name = "go-dev-env";

    buildInputs = with pkgs; [
      go
      go-tools
      gopls
      delve
    ];
  }
