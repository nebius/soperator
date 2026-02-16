#!/usr/bin/env python3

import configparser
import os
import pathlib
import tempfile


def main() -> int:
    config_path = pathlib.Path(os.environ["NFS_CLIENT_CONFIG_PATH"])
    section_name = "NFSMount_Global_Options"
    option_name = "nconnect"
    option_value = "16"

    cfg = configparser.ConfigParser(
        delimiters=("=",),
        interpolation=None,
        strict=False,
    )
    cfg.optionxform = str  # preserve key case

    if config_path.exists():
        with config_path.open("r", encoding="utf-8") as config_file:
            cfg.read_file(config_file)

    changed = False

    if not cfg.has_section(section_name):
        cfg.add_section(section_name)
        changed = True

    current_value = cfg.get(section_name, option_name, fallback=None)
    if current_value != option_value:
        cfg.set(section_name, option_name, option_value)
        changed = True

    if not changed:
        print(f"No change needed for {config_path}")
        return 0

    config_path.parent.mkdir(parents=True, exist_ok=True)

    fd, tmp_name = tempfile.mkstemp(
        prefix=".nfs-client-config.",
        dir=str(config_path.parent),
        text=True,
    )

    try:
        with os.fdopen(fd, "w", encoding="utf-8") as temp_file:
            cfg.write(temp_file, space_around_delimiters=False)

        os.replace(tmp_name, config_path)
        print(f"Updated {config_path}")
    finally:
        if os.path.exists(tmp_name):
            os.unlink(tmp_name)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
