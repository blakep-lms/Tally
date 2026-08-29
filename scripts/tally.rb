# Tally — Homebrew formula (tap: blakep-lms/tools, formula: tally)
#   brew tap blakep-lms/tools && brew install tally
# Installs the Tally CLI + scripts and symlinks `tally` onto PATH.
class Tally < Formula
  desc "Local-first passive time tracking: watch windows, bucket time, export for invoicing"
  homepage "https://github.com/blakep-lms/tally"
  url "https://github.com/blakep-lms/tally/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "REPLACE_WITH_RELEASE_SHA256"
  license "MIT"

  depends_on "python@3.12"

  def install
    scripts = %w[
      tally.py watch.py store.py tally_setup.py tally_ctl.py tally_menu_v2.py
      tally_dashboard.py tally_export.py timeline.py suggest_rules.py doctor.py
      bucket_server.py bucket-editor.html install_menubar.py
    ]
    libexec.install scripts
    bin.install "tally.py" => "tally"
  end

  def post_install
    ohai "Tally installed. Run `tally setup` to create your buckets."
  end

  test do
    assert_match "tally", shell_output("#{bin}/tally --help", 0)
  end
end
