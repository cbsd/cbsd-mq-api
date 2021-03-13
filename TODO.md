- jail and bhyve valid pass ptions via configfile + regexp, e.g:

jail_pass_args=['ver','pkglist','allow_raw_sockets']
ver_regex="12|13"
allow_raw_sockets_regex="0|1"
pkglist_regex="^[aA-zZ0-1 ]...*$"

