{
  description = "cpgo: checksum-verified mirroring copy tool";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs = { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" ];
    in
    {
      packages = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in
        {
          default = pkgs.buildGoModule {
            pname = "cpgo";
            version = "0.1.0";
            src = ./.;
            # logrus (and its golang.org/x/sys dependency) is now vendored,
            # so this needs a real hash. `nix build` will fail with the
            # correct value on first run; paste it in here once you have it.
            vendorHash = pkgs.lib.fakeHash;
          };
        });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/cpgo";
        };
      });
    };
}
