{
  description = "Domain — a JJK-themed language for Advent of Code: tree-walking interpreter plus an optimizing Go compiler backend, in one `domain` binary";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system:
        f system nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (system: pkgs: rec {
        domain = pkgs.buildGoModule {
          pname = "domain";
          version = "0.4.0";
          src = self;

          # Vendor hash covers charm.land/bubbletea/v2, charm.land/bubbles/v2,
          # and their transitive dependencies (the REPL's line editor).
          vendorHash = "sha256-BaaZD69s6Xmum7SRRUSK6YbiNWsg5w8ChtBxPirPINU=";
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
              --suffix PATH : ${pkgs.lib.makeBinPath [ pkgs.go ]}
          '';

          # The docs site's playground (docs/wasm/domain.wasm) is a build
          # artifact, deliberately not committed — see docs/wasm/README.md.
          # Without this, the Nix package would embed an empty wasm/
          # directory and ship with no Run buttons. build.sh needs only the
          # Go toolchain and the module's own vendored deps, both already
          # present by preBuild, so this runs fully offline in the sandbox.
          #
          # Run via `bash` rather than `./docs/wasm/build.sh`: the sandbox has
          # no /usr/bin/env, so the script's own #!/usr/bin/env bash shebang
          # cannot resolve there.
          preBuild = ''
            bash docs/wasm/build.sh
          '';

          # The repo's test suite is an interpreter-vs-binary oracle: it
          # compiles dozens of programs with a nested `go build`, which is
          # awkward inside the Nix build sandbox. Run `go test ./...` (or
          # `nix develop` first) for the full suite.
          doCheck = false;

          meta = {
            description = "Describe what, let the compiler choose how — AoC pipeline language with algorithm-substituting optimizer and Go codegen";
            homepage = "https://github.com/RFuller25/domain";
            license = pkgs.lib.licenses.mit;
            mainProgram = "domain";
          };
        };
        # Neovim/Vim syntax highlighting as an installable plugin:
        #   programs.neovim.plugins = [ domain.packages.${system}.domain-nvim ];
        domain-nvim = pkgs.vimUtils.buildVimPlugin {
          pname = "domain-nvim";
          version = "0.4.0";
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
          packages = [ pkgs.go pkgs.gopls pkgs.gotools ];
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
