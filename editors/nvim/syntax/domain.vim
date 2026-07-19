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
syn match domainHole /{[^}]*}/ contained

" Themed pipeline keywords (statement heads). Matched with \ze so the colon
" and operation phrase keep their own styling. Multi-word forms first.
syn match domainKeyword /^\s*\%(Cursed Energy\|Cursed Technique\|Channeled Energy\|Maximum Technique\|Domain Expansion\|Reverse Cursed Technique\|Simple Domain\|Binding Vow\|Reveal\|Channel\|Shikigami\)\>/

" Indented named arguments: Using:, Mode:, Seed:, From:, k: ...
syn match domainArgKey /^\s\+\zs[A-Za-z_][A-Za-z0-9_]*\ze:/

" The Go-backed primitives (the operation phrases). Title Case, so they never
" collide with the lowercase builtins. Multi-word phrases first so the longest
" name wins.
syn match domainPrimitive /\<\%(Sum Each Group\|Split Each\|Split Fields\|Extract Integers\|Ragged Columns\|Map Each\|Map Cells\|Find Cells\|Match Pattern\|Take Item\|Select Top\|Count Matching\|Count Cells\|Count By\|Min By\|Max By\|Sort By\|Group By\|All Pairs\|Merge Ranges\|Flood Fill\|Connected Components\|Convert\|Split\|Window\|Flatten\|Enumerate\|Filter\|Unique\|Transpose\|Apply\|Quicksort\|Sort\|Combinations\|Permutations\|Subsets\|BFS\|Dijkstra\|Reverse\|Product\|Intersect\|Union\|Difference\|Fold\|Join\|Sum\|Count\|Max\|Min\|Emit\)\>/

" Simple Domain loop drivers, and operation connector words.
syn match domainLoop /\<\%(Iterate Until Fixed Point\|Repeat\|While\)\>/
syn keyword domainPreposition by from to with into of

" The expression layer.
syn match domainArrow /->/
syn match domainOperator /<=\|>=\|!=\|[-+*/%=<>]/
syn keyword domainLogical and or not if then else
syn match domainNumber /\<\d\+\>/

" Builtin functions, only in call position (lowercase, so they never collide
" with the Title Case operation words above).
syn match domainBuiltin /\<\%(abs\|at\|band\|bor\|bxor\|ceil\|cells\|col\|cols\|concat\|contains\|dirs4\|drop\|first\|floor\|frombin\|gcd\|get\|has\|inbounds\|item\|last\|lcm\|length\|list\|manhattan\|maxcol\|maxrow\|max\|mincol\|minrow\|min\|modinv\|modpow\|name\|neighbors4\|neighbors8\|occurrences\|padd\|pcol\|point\|prow\|put\|repeats\|reverse\|rotl\|rotr\|round\|rows\|row\|set\|shl\|shr\|sign\|solve2x2\|sparse\|sqrt\|sum\|take\|tofloat\|toint\|totext\)\ze\s*(/

" Mode: values, order modifiers, and value-kind type names.
syn match domainMode /\%(Mode:\s*\)\@<=\%(One\|Each\|Filter\|Count\|First\|Map\)\>/
syn keyword domainOrder Ascending Descending
syn keyword domainType Int Text Float Bool List Tuple Record Map Set Grid Sparse

hi def link domainComment  Comment
hi def link domainString   String
hi def link domainEscape   SpecialChar
hi def link domainHole     Special
hi def link domainKeyword     Statement
hi def link domainPrimitive   Function
hi def link domainLoop        Repeat
hi def link domainPreposition Keyword
hi def link domainArgKey      Identifier
hi def link domainArrow       Operator
hi def link domainOperator    Operator
hi def link domainLogical     Keyword
hi def link domainNumber      Number
hi def link domainBuiltin     Function
hi def link domainMode        Constant
hi def link domainOrder       Constant
hi def link domainType        Type

let b:current_syntax = "domain"
