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
            # No external Go dependencies (stdlib only), so this is stable
            # even if go.sum doesn't exist yet.
            vendorHash = null;
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
