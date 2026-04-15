{
  description = "Go bindings for the ECOS conic solver";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
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
          ecosLib = pkgs.stdenv.mkDerivation {
            pname = "ecos";
            version = "2.0.10";

            src = pkgs.fetchFromGitHub {
              owner = "embotech";
              repo = "ecos";
              tag = "v2.0.10";
              hash = "sha256-WMgqDc+XAY3g2wwlefjJ0ATxR5r/jL971FZKtxsunnU=";
            };

            buildPhase = ''
              runHook preBuild
              make all shared
              runHook postBuild
            '';

            doCheck = false;

            installPhase = ''
              runHook preInstall
              mkdir -p $out/lib $out/include
              cp lib*.a lib*.so* $out/lib
              cp -r include/* $out/include/
              cp external/SuiteSparse_config/*.h $out/include/
              cp external/amd/include/*.h $out/include/
              cp external/ldl/include/*.h $out/include/
              runHook postInstall
            '';
          };
        in
        {
          default = pkgs.buildGoModule {
            pname = "ecos-golang";
            inherit version;
            src = ./.;
            vendorHash = null;

            buildInputs = [ ecosLib ];
            nativeBuildInputs = [ pkgs.pkg-config ];

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
            ];
          };
        }
      );

      defaultPackage = forAllSystems (system: self.packages.${system}.default);
    };
}
