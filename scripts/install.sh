#!/bin/bash
# Tally installer — curl | bash
#   curl -sSL https://tally.dev/install.sh | bash
# Downloads Tally to ~/.local/lib/tally, symlinks `tally` onto PATH, runs first-run setup.
set -euo pipefail

TALLY_REPO="${TALLY_REPO:-https://raw.githubusercontent.com/blakep-lms/tally/main}"
INSTALL_DIR="$HOME/.local/lib/tally"
BIN_DIR="$HOME/.local/bin"

echo "==> Installing Tally (local-first time tracking)"
mkdir -p "$INSTALL_DIR" "$BIN_DIR"

FILES="tally.py watch.py store.py tally_setup.py tally_ctl.py tally_menu_v2.py
       tally_dashboard.py tally_export.py timeline.py suggest_rules.py doctor.py
       bucket_server.py bucket-editor.html"

for f in $FILES; do
  curl -fsSL "$TALLY_REPO/scripts/$f" -o "$INSTALL_DIR/$f"
  echo "    $f"
done
chmod +x "$INSTALL_DIR"/*.py

ln -sf "$INSTALL_DIR/tally.py" "$BIN_DIR/tally"
echo "==> Added: $BIN_DIR/tally"

# PATH hint
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "==> Add to your shell:  export PATH=\"\$HOME/.local/bin:\$PATH\"" ;;
esac

# First-run: macOS screen-recording/System Events permission prompt happens here.
echo "==> First-run setup"
python3 "$INSTALL_DIR/tally_setup.py"

echo "==> Done. Try:  tally status"
