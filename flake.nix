{
  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  outputs = { nixpkgs, ... }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs {
        inherit system;
        config.allowUnfree = true;
      };
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        name = "chaos-monkey";
        hardeningDisable = [ "fortify" ];
        packages = with pkgs; [
          go
          terraform
          kind
          kubernetes-code-generator
          kubernetes-helm
          curl
          cocogitto
          jq
        ];
      };
    };
}
