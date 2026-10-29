#!/usr/bin/env bash
set -euo pipefail
[ "${OBEY_SDK_TEST_CONTAINER:-}" = 1 ] && [ -f /.dockerenv ] || { echo 'Disposable Docker fixture required' >&2; exit 2; }
[ ! -e "$HOME/.obey/installer/bin/obey" ] || { echo 'Obey already exists; use a fresh fixture' >&2; exit 1; }
festival install obey --channel stable --no-shell-setup --no-restart --json \
 > /evidence/install.json 2> /evidence/installer.log
festival list --json > /evidence/packages.json
festival marketplace list --json > /evidence/marketplaces.json
festival resolve obey > /evidence/obey-path.txt
python3 /sdk/scripts/installed/verify-install.py
