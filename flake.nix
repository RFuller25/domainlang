{
  description = "Domain — a JJK-themed language for Advent of Code: tree-walking interpreter plus an optimizing Go compiler backend, in one `domain` binary";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system:
        f system nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (system: pkgs: let
        # go.mod requires go >= 1.25; nixos-25.05's default `go` is 1.24.10,
        # so pin the toolchain via `.override` (buildGoModule's `go` is a
        # callPackage-bound argument, not a settable field on the derivation
        # attrset below — passing `go = ...;` there is silently ignored).
        buildGoModule = pkgs.buildGoModule.override { go = pkgs.go_1_25; };
      in rec {
        domain = buildGoModule {
          pname = "domain";
          version = "0.3.0";
          src = self;

          vendorHash = "sha256-VdBnS/3z3PRMoZ4vbtl7YBZrQcpG1pNQhVqkDRMFZ1Q=";
          subPackages = [ "cmd/domain" ];

          # `domain <file> <args...>` (the compiler path) shells out to the
          # Go toolchain via `go build`, so the installed binary must always
          # be able to find `go` — even on systems where Go isn't otherwise
          # installed. The wrapper below embeds go's store path, which
          # buildGoModule forbids by default (disallowedReferences); this is
          # the sanctioned opt-out. The price is the Go toolchain in the
          # runtime closure — inherent to a compiler that invokes `go build`.
          allowGoReference = true;
          nativeBuildInputs = [ pkgs.makeWrapper ];
          postInstall = ''
            wrapProgram $out/bin/domain \
              --suffix PATH : ${pkgs.lib.makeBinPath [ pkgs.go_1_25 ]}
          '';

          # The repo's test suite is an interpreter-vs-binary oracle: it
          # compiles dozens of programs with a nested `go build`, which is
          # awkward inside the Nix build sandbox. Run `go test ./...` (or
          # `nix develop` first) for the full suite.
          doCheck = false;

          meta = {
            description = "Describe what, let the compiler choose how — AoC pipeline language with algorithm-substituting optimizer and Go codegen";
            homepage = "https://github.com/RFuller25/domainlang";
            license = pkgs.lib.licenses.mit;
            mainProgram = "domain";
          };
        };
        # Neovim/Vim syntax highlighting as an installable plugin:
        #   programs.neovim.plugins = [ domain.packages.${system}.domain-nvim ];
        domain-nvim = pkgs.vimUtils.buildVimPlugin {
          pname = "domain-nvim";
          version = "0.3.0";
          src = ./editors/nvim;
        };

        default = domain;
      });

      apps = forAllSystems (system: pkgs: rec {
        domain = {
          type = "app";
          program = "${self.packages.${system}.domain}/bin/domain";
        };
        default = domain;
      });

      devShells = forAllSystems (system: pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go_1_25 pkgs.gopls pkgs.gotools ];
        };
      });

      checks = forAllSystems (system: pkgs: {
        # `nix flake check` at least proves the package builds; the full
        # test suite runs outside the sandbox (see doCheck above).
        build = self.packages.${system}.domain;
      });

      overlays.default = final: prev: {
        domain = self.packages.${final.stdenv.hostPlatform.system}.domain;
      };
    };
}