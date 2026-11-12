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
      packages = forAllSystems (system: pkgs:
        let
          # One definition, two packages. `domain` (the default) can compile:
          # `domain build` shells out to `go build`, so its wrapper carries the
          # Go toolchain in the runtime closure — about 228 MB of the package's
          # 254 MB. `domain-lite` is the same binary without the wrapper: the
          # interpreter, the REPL, the editor, `fmt`, `doc` and the rest, for
          # someone who never compiles, and a closure of a few tens of MB.
          build = withGo: pkgs.buildGoModule {
          pname = if withGo then "domain" else "domain-lite";
          version = "0.6.0";
          # What the build copies into the store. The Go build needs neither the
          # tests, nor the benchmark suite, nor the research and planning
          # material, and every file left in `src` is one more thing to hash,
          # copy and rebuild on. `docs/` and `editors/` stay whole apart from
          # these: the binary embeds their documentation and extension files.
          # `go mod vendor` sees the same dependency set without the tests, so
          # `vendorHash` is unchanged. maybeMissing: a flake fetched as a
          # GitHub tarball has these paths removed by `.gitattributes`
          # (export-ignore) before this ever evaluates.
          src =
            let fs = pkgs.lib.fileset;
            in fs.toSource {
              root = ./.;
              fileset = fs.difference ./. (fs.unions [
                (fs.fileFilter (f: pkgs.lib.hasSuffix "_test.go" f.name) ./.)
                (fs.maybeMissing ./selfhost)
                (fs.maybeMissing ./bench)
                (fs.maybeMissing ./testdata)
                (fs.maybeMissing ./codegen/testdata)
                (fs.maybeMissing ./docs/testdata)
                (fs.maybeMissing ./docs/superpowers)
                (fs.maybeMissing ./diag/testdata)
                # The programs the docs and README point at. The binary embeds
                # none of them (the playground's gallery is docs/gallery.json),
                # and their .expected files exist only for the tests.
                (fs.maybeMissing ./examples)
                (fs.maybeMissing ./challenges)
                (fs.maybeMissing ./bloat_findings.md)
              ]);
            };

          # Vendor hash covers charm.land/bubbletea/v2, charm.land/bubbles/v2,
          # and their transitive dependencies (the REPL's line editor).
          vendorHash = "sha256-BaaZD69s6Xmum7SRRUSK6YbiNWsg5w8ChtBxPirPINU=";
          subPackages = [ "cmd/domain" ];

          # No symbol table and no DWARF in the shipped CLI: about a quarter
          # off the binary. Panic traces still carry function names.
          ldflags = [ "-s" "-w" ];

          # buildGoModule builds the dependencies in a *separate* derivation
          # (`domain-<version>-go-modules`) whose only job is to run
          # `go mod vendor`, and — surprisingly — it inherits `preBuild` from
          # here (nixpkgs says as much in pkgs/build-support/go/module.nix:
          # "The following inheritance behavior is not trivial to expect").
          # Worse, it runs that hook as the *first* thing in its build phase,
          # before `go mod vendor` has fetched anything.
          #
          # So the playground build below would run twice: once in the real
          # build, where it belongs, and once in the vendor derivation, where
          # there are no dependencies for it to compile against yet. That
          # second run is pure waste on a good day, and on a bad one it is a
          # build failure: if anything has left a `vendor/` directory in the
          # source tree, Go sees it, switches to `-mod=vendor`, finds no
          # `vendor/modules.txt` to go with it, and stops with
          #
          #     go: inconsistent vendoring in /build/source:
          #       charm.land/bubbles/v2@v2.1.1: is explicitly required in
          #       go.mod, but not marked as explicit in vendor/modules.txt
          #
          # — a message about vendoring, from the derivation whose entire
          # purpose is to *create* the vendoring, listing every dependency in
          # go.mod. `overrideModAttrs` is the sanctioned way to say "that hook
          # is for the real build only". Clearing it does not affect
          # vendorHash: the vendor derivation's output is the `vendor/` tree
          # `go mod vendor` writes, which the playground build never touches.
          overrideModAttrs = _finalAttrs: _prevAttrs: {
            preBuild = "";
          };

          # `domain <file> <args...>` (the compiler path) shells out to the
          # Go toolchain via `go build`, so the installed binary must always
          # be able to find `go` — even on systems where Go isn't otherwise
          # installed. The wrapper below embeds go's store path, which
          # buildGoModule forbids by default (disallowedReferences); this is
          # the sanctioned opt-out. The price is the Go toolchain in the
          # runtime closure — inherent to a compiler that invokes `go build`.
          allowGoReference = withGo;
          nativeBuildInputs = pkgs.lib.optional withGo pkgs.makeWrapper;
          postInstall = pkgs.lib.optionalString withGo ''
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
          #
          # `overrideModAttrs` above keeps this out of the vendor derivation,
          # where the deps it needs do not exist yet.
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
            homepage = "https://github.com/RFuller25/domainlang";
            license = pkgs.lib.licenses.mit;
            mainProgram = "domain";
          };
        };
        in rec {
          domain = build true;
          domain-lite = build false;

        # Neovim/Vim syntax highlighting as an installable plugin:
        #   programs.neovim.plugins = [ domain.packages.${system}.domain-nvim ];
        domain-nvim = pkgs.vimUtils.buildVimPlugin {
          pname = "domain-nvim";
          version = "0.6.0";
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
