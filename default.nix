let
  commit = builtins.getEnv "COMMIT";
  sha256 = builtins.getEnv "SHA";
  pkgs =
    import (builtins.fetchTarball {
      url = "https://github.com/nixos/nixpkgs/archive/${commit}.tar.gz";
      sha256 = sha256;
    })
    {
      config.allowBroken = true;
      config.allowUnfree = true;
    };

  lib = pkgs.lib;
  filter =
    lib.attrsets.filterAttrsRecursive (name: value: name != null && value != null && value != {});

  map = pkgs.lib.attrsets.mapAttrs (
    name: value:
      if !(builtins.tryEval value).success
      then null
      else if lib.isDerivation value
      then mapPackage value
      else if value.recurseForDerivations or false
      then pkgs.recurseIntoAttrs (map value)
      else null
  );

  mapPackage = value: let
    hasAttr = field:
      (builtins.tryEval (
        if builtins.hasAttr field value
        then builtins.deepSeq (value."${field}") true
        else false
      ))
      .value
      == true;
  in
    if hasAttr "version" # TODO: some old packages did not have version, they used name
    then {
      pname = value.pname or null;
      version = builtins.toString value.version; # ruby's "version" is not a string
    }
    else null;
in
  builtins.toJSON (filter (map pkgs))
