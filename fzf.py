"""fzf wrapper for theme selection with live preview."""

import os
import shlex
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

    fzf needs the TTY for its interactive UI (keyboard input, screen drawing).
    We pipe items via a temp file and capture the selection via another temp file,
    letting fzf inherit stdio directly for full terminal access.
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

    # Write items to a temp file, pipe it into fzf via shell so fzf's
    # stdin/stdout/stderr are connected to the real terminal.
    with tempfile.NamedTemporaryFile(mode="w", suffix=".txt", delete=False) as items_f:
        items_f.write("\n".join(items))
        items_path = items_f.name

    with tempfile.NamedTemporaryFile(mode="w", suffix=".txt", delete=False) as out_f:
        out_path = out_f.name

    try:
        # Build a shell command: cat items | fzf ... > output
        fzf_args = " ".join(shlex.quote(c) for c in cmd)
        shell_cmd = f"cat {shlex.quote(items_path)} | {fzf_args} > {shlex.quote(out_path)}"

        returncode = os.system(shell_cmd)

        if returncode == 0:
            selection = open(out_path).read().strip()
            return selection if selection else None
        return None
    finally:
        os.unlink(items_path)
        os.unlink(out_path)


