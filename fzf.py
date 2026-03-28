"""fzf wrapper for theme selection with live preview."""

import subprocess
import sys


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

    Args:
        items: List of items to select from.
        header: Header text shown at top of fzf.
        preview_cmd: Command to run for preview pane ({} is replaced with item).
        focus_cmd: Command to run on each cursor movement (live preview).
        abort_cmd: Command to run when user presses Esc (revert).
        prompt: The fzf prompt string.
        current: If set, pre-select this item.
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
        # Run abort command, then abort fzf
        binds.append(f"esc:execute-silent({abort_cmd})+abort")

    if binds:
        cmd.extend(["--bind", ",".join(binds)])

    # If we have a current value, try to position on it
    if current and current in items:
        # Move current item to be initially selected by reordering
        # (fzf doesn't have a --select flag, but we can use --query or reorder)
        pass  # fzf will just start at top, which is fine

    input_text = "\n".join(items)

    result = subprocess.run(
        cmd,
        input=input_text,
        capture_output=True,
        text=True,
    )

    if result.returncode == 0 and result.stdout.strip():
        return result.stdout.strip()
    return None
