{
  description = "Go bindings for the ECOS conic solver";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    # Include the vendored ECOS git submodule in `self`. Without this,
    # `src = ./.` in a flake omits submodule contents and the preBuild
    # `make -C ecos` step has nothing to compile.
    self.submodules = true;
  };

  outputs =
    { self, nixpkgs }:
    let
      lastModifiedDate = self.lastModifiedDate or self.lastModified or "19700101";
      version = builtins.substring 0 8 lastModifiedDate;
      supportedSystems = [
        "x86_64-linux"
        "x86_64-darwin"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          default = pkgs.buildGoModule {
            pname = "ecos-golang";
            inherit version;
            # `self.submodules = true` in inputs ensures this tree includes
            # the vendored ECOS sources so `make -C ecos` in preBuild works.
            src = ./.;
            vendorHash = null;

            nativeBuildInputs = [ pkgs.gnumake ];

            # Build the vendored ECOS static libs before compiling Go. cgo then
            # picks them up via -L${SRCDIR}/../ecos in ecosgo/ecos.go.
            preBuild = ''
              make -C ecos
            '';

            ldflags = [
              "-s"
              "-w"
            ];
          };
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          default = pkgs.mkShell {
            inputsFrom = [ self.packages.${system}.default ];

            packages = with pkgs; [
              go
              gopls
              gotools
              go-tools
              gnumake
            ];
          };
        }
      );

      defaultPackage = forAllSystems (system: self.packages.${system}.default);
    };
}
