"""fzf wrapper for theme selection with live preview."""

import os
import shlex
import subprocess
import tempfile


def run_fzf(
    items: list[str],
    *,
    header: str = "",
    preview_cmd: str | None = None,
    focus_cmd: str | None = None,
    abort_cmd: str | None = None,
    prompt: str = "theme> ",
    current: str | None = None,
) -> str | None:
    """Run fzf and return the selected item, or None if cancelled.

    fzf needs /dev/tty for keyboard input and screen drawing. We pass items
    via a temp file (shell < redirect) and capture the selection via another
    temp file (shell > redirect). fzf's own stdin/stderr go to /dev/tty
    for the interactive UI.
    """
    cmd = [
        "fzf",
        "--ansi",
        "--no-multi",
        "--prompt", prompt,
        "--border", "rounded",
        "--border-label", " themer ",
    ]

    if header:
        cmd.extend(["--header", header])

    if preview_cmd:
        cmd.extend(["--preview", preview_cmd])
        cmd.extend(["--preview-window", "right:50%:wrap"])

    binds = []
    if focus_cmd:
        binds.append(f"focus:execute-silent({focus_cmd})")
    if abort_cmd:
        binds.append(f"esc:execute-silent({abort_cmd})+abort")

    if binds:
        cmd.extend(["--bind", ",".join(binds)])

    # Write items to temp file
    with tempfile.NamedTemporaryFile(mode="w", suffix=".list", delete=False) as f:
        f.write("\n".join(items) + "\n")
        items_path = f.name

    out_fd, out_path = tempfile.mkstemp(suffix=".selection")
    os.close(out_fd)

    try:
        # Shell command: fzf reads items from file, writes selection to file,
        # and gets /dev/tty for its interactive UI automatically (fzf opens
        # /dev/tty itself when stdin is not a terminal).
        fzf_args = " ".join(shlex.quote(c) for c in cmd)
        shell_cmd = f"{fzf_args} < {shlex.quote(items_path)} > {shlex.quote(out_path)}"

        returncode = subprocess.call(shell_cmd, shell=True)

        if returncode == 0:
            with open(out_path) as f:
                selection = f.read().strip()
            return selection if selection else None
        return None
    finally:
        for p in (items_path, out_path):
            try:
                os.unlink(p)
            except OSError:
                pass
