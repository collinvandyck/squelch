
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
# Wrap each command in a function, which can check NOSQUELCH and its arguments when it runs. An alias of the same name
# would win over the function, so drop it. The function keyword keeps the name from being alias-expanded before the
# unalias runs.
{{- range .Cmds}}
(( ${+aliases[{{.}}]} )) && unalias {{.}}
function {{.}} {
  if [[ -n ${NOSQUELCH-} ]]{{range $.SkipArgs}} || (( ${argv[(Ie){{.}}]} )){{end}}; then
    command {{.}} "$@"
  else
    squelch run {{.}} "$@"
  fi
}
{{- end}}
