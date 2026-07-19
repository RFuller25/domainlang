" Filetype settings for Domain.
" Indentation is significant and tabs are a lex error, so force spaces.
if exists("b:did_ftplugin")
  finish
endif
let b:did_ftplugin = 1

setlocal expandtab
setlocal shiftwidth=4
setlocal softtabstop=4
setlocal comments=:#
setlocal commentstring=#\ %s

let b:undo_ftplugin = "setlocal expandtab< shiftwidth< softtabstop< comments< commentstring<"
