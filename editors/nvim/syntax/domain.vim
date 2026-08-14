" Vim/Neovim syntax highlighting for the Domain language (.domain).
" Mirrors the TextMate grammar in editors/vscode/syntaxes/.
if exists("b:current_syntax")
  finish
endif

" Comments run from # to end of line.
syn match domainComment "#.*$" contains=@Spell

" Strings, with the standard escapes and Match Pattern typed holes.
syn region domainString start=/"/ skip=/\\./ end=/"/ contains=domainEscape,domainHole
syn match domainEscape /\\[nt\\"]/ contained
" One level of nesting: a repeated group's inner template has holes of its own,
" `{ds:( {n:int} {c:word} )+ sep=", "}`, and /{[^}]*}/ stopped at the first one.
syn match domainHole /{\%([^{}]\|{[^{}]*}\)*}/ contained

" Themed pipeline keywords (statement heads). Matched with \ze so the colon
" and operation phrase keep their own styling. Multi-word forms first.
syn match domainKeyword /^\s*\%(Reverse Cursed Technique\|Maximum Technique\|Channeled Energy\|Cursed Technique\|Domain Expansion\|Cursed Energy\|Innate Domain\|Simple Domain\|Binding Vow\|Shikigami\|Channel\|Reveal\|Part\)\>/

" Foreign-language blocks: `Domain Expansion: Python` (or Go/rask/cRust/Weave) and
" everything indented beneath it, which is that language's source rather than
" Domain. \z( \) captures the opener's indentation and \z1 ends the region at
" the first non-blank line that is not indented past it — the same rule the
" lexer applies. The body is left unhighlighted: colouring another language as
" Domain is worse than not colouring it, and vim has no portable way to embed
" four grammars that may or may not be installed.
syn region domainForeign matchgroup=domainForeignHead
      \ start=/^\z(\s*\)\%(\%(Cursed Energy\|Cursed Technique\|Channeled Energy\|Maximum Technique\|Domain Expansion\|Reverse Cursed Technique\|Simple Domain\|Binding Vow\|Reveal\)\s*:\s*\)\=\%(Python\|Go\|rask\|cRust\|Weave\)\>\%(\s*:.*\)\=$/
      \ end=/^\z1\ze\S/me=s-1
      \ keepend

" Indented named arguments: Using:, Mode:, Seed:, From:, k: ...
syn match domainArgKey /^\s\+\zs[A-Za-z_][A-Za-z0-9_]*\ze:/

" Local bindings: `Consider NAME As …` / `Consider NAME Of …`. The keywords are
" matched case-insensitively (\c), like the parser does, and the bound name is
" highlighted as the definition it is. Only this exact shape is a binding, so a
" phrase that merely starts with the word keeps its ordinary colouring.
syn match domainBind /^\s*\c\<consider\>\ze\s\+[A-Za-z_][A-Za-z0-9_]*\s\+\c\%(as\|of\)\>/
syn match domainBindName /^\s*\c\<consider\>\s\+\zs[A-Za-z_][A-Za-z0-9_]*\ze\s\+\c\%(as\|of\)\>/
syn match domainBindPrep /^\s*\c\<consider\>\s\+[A-Za-z_][A-Za-z0-9_]*\s\+\zs\c\%(as\|of\)\>/

" The Go-backed primitives (the operation phrases). Title Case, so they never
" collide with the lowercase builtins. Multi-word phrases first so the longest
" name wins.
syn match domainPrimitive /\<\%(Convert To Sparse Grid\|Connected Components\|Convert To Integers\|Convert To Entries\|Convert To Floats\|Convert To Edges\|Convert To Graph\|Extract Integers\|Topological Sort\|Convert To Grid\|Convert To Rows\|Convert To Map\|Convert To Set\|Count Matching\|Filter Entries\|Ragged Columns\|Sliding Reduce\|Sum Each Group\|Match Pattern\|Shortest Path\|Combinations\|Merge Ranges\|Permutations\|Select Top K\|Split Fields\|Binding Vow\|Count Cells\|Read Source\|Rotate Grid\|Difference\|Drop While\|Find Cells\|Find Cycle\|Find Index\|Flood Fill\|Map Values\|Product By\|Split Each\|Take While\|All Pairs\|Enumerate\|Flip Grid\|Intersect\|Map Cells\|Partition\|Quicksort\|Take Item\|Transpose\|Count By\|Dijkstra\|Group By\|Map Each\|Pad Grid\|Combine\|Explore\|Flatten\|Iterate\|Product\|Reverse\|Sort By\|Subgrid\|Subsets\|Equals\|Filter\|Max By\|Min By\|Reduce\|Sum By\|Unfold\|Unique\|Values\|Window\|Apply\|Chunk\|Count\|Holds\|Pairs\|Range\|Split\|Union\|Emit\|Find\|Fold\|Join\|Scan\|Sort\|All\|Any\|BFS\|Max\|Min\|Sum\|Zip\)\>/

" The prelude's Shikigami. A Shikigami is called by its bare name, so these
" read like primitives at a call site and are coloured as the operations they
" are. Generated with the lists above.
syn match domainShikigami /\<\%(Digit Grid\|Top K Sum\|Blocks\|Lines\|Ints\)\>/

" The standard source and sink targets: `Cursed Energy: stdin`, `Reveal: stdout`.
syn keyword domainTarget stdin stdout

" Simple Domain loop drivers, and operation connector words.
syn match domainLoop /\<\%(Iterate Until Fixed Point\|Repeat\|While\)\>/
syn keyword domainPreposition by from to with into of

" The expression layer.
syn match domainArrow /->/
syn match domainOperator /:=\|<=\|>=\|!=\|[-+*/%=<>]/
syn keyword domainLogical and or not if then else also
syn match domainNumber /\<\d\+\>/

" Builtin functions, only in call position (lowercase, so they never collide
" with the Title Case operation words above).
syn match domainBuiltin /\<\%(occurrences\|cellpoints\|difference\|emptygraph\|fromdigits\|neighbors4\|neighbors8\|startswith\|trimprefix\|trimsuffix\|chebyshev\|emptylist\|enumerate\|factorial\|flipedges\|intersect\|manhattan\|neighbors\|transpose\|contains\|divisors\|emptymap\|emptyset\|endswith\|frombase\|inbounds\|padright\|popcount\|solve2x2\|subgraph\|textjoin\|weightor\|addedge\|addnode\|around4\|around8\|bandall\|bxorall\|deledge\|edgesof\|entries\|flatten\|frombin\|fromhex\|hasedge\|indexof\|isalpha\|isdigit\|islower\|isprime\|isupper\|padleft\|product\|repeats\|replace\|reverse\|testbit\|tofloat\|windows\|borall\|charat\|choose\|concat\|degree\|digits\|divmod\|haskey\|insert\|length\|maxcol\|maxrow\|mincol\|minrow\|modinv\|modpow\|pscale\|record\|repeat\|sparse\|tobase\|tolist\|totext\|unique\|values\|weight\|atan2\|cells\|chars\|chunk\|clamp\|dirs4\|dirs8\|edges\|first\|floor\|getor\|graph\|hypot\|isqrt\|log10\|lower\|nodes\|point\|range\|round\|setat\|slice\|split\|tobin\|tohex\|toint\|tomap\|toset\|trunc\|tuple\|union\|upper\|words\|band\|bnot\|bxor\|ceil\|cols\|drop\|fill\|item\|keys\|last\|list\|log2\|padd\|pcol\|prow\|psub\|rotl\|rotr\|rows\|sign\|size\|sort\|sqrt\|take\|trim\|with\|abs\|and\|bor\|chr\|col\|cos\|crt\|del\|exp\|gcd\|get\|has\|lcm\|log\|max\|min\|mod\|not\|ord\|pow\|put\|row\|set\|shl\|shr\|sin\|sum\|tan\|xor\|zip\|at\|or\)\ze\s*(/

" Mode: values, order modifiers, and value-kind type names.
syn match domainMode /\%(Mode:\s*\)\@<=\%(One\|Each\|Try\|Scan\|Filter\|Count\|First\|Map\)\>/
syn keyword domainOrder Ascending Descending
syn keyword domainType Int Text Float Bool List Tuple Record Map Set Grid Sparse

hi def link domainComment  Comment
hi def link domainString   String
hi def link domainEscape   SpecialChar
hi def link domainHole     Special
hi def link domainKeyword     Statement
hi def link domainPrimitive   Function
hi def link domainShikigami   Function
hi def link domainTarget      Constant
hi def link domainLoop        Repeat
hi def link domainPreposition Keyword
hi def link domainArgKey      Identifier
hi def link domainBind        Statement
hi def link domainBindName    Identifier
hi def link domainBindPrep    Statement
hi def link domainArrow       Operator
hi def link domainOperator    Operator
hi def link domainLogical     Keyword
hi def link domainNumber      Number
hi def link domainBuiltin     Function
hi def link domainMode        Constant
hi def link domainOrder       Constant
hi def link domainType        Type
hi def link domainForeign     Normal
hi def link domainForeignHead Statement

let b:current_syntax = "domain"
