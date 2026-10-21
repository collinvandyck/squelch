
# squelch's own completion only knows about squelch. Once the words reach "squelch run", complete the rest as if
# it were typed on its own, so the wrapped command's completion takes over.
_squelch_run() {
  if (( CURRENT > 2 )) && [[ ${words[2]} == run ]]; then
    words=("${(@)words[3,-1]}")
    (( CURRENT -= 2 ))
    _normal
  else
    _squelch
  fi
}
compdef _squelch_run squelch
{{range .}}
alias {{.}}='squelch run {{.}}'
{{- end}}
