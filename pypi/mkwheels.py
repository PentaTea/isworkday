#!/usr/bin/env python3
"""Pack the released Go binaries (bin/isworkday-<Os>-<arch>) into platform wheels.
stdlib only; reproducible (fixed zip timestamps).
usage: mkwheels.py VERSION ASSET_DIR OUT_DIR"""
import base64, hashlib, pathlib, struct, sys, zipfile

NAME = "isworkday"

# The macOS minimum ("13_0") from the binary's LC_BUILD_VERSION; it rises with the Go toolchain.
def macos_min(exe):
    off = 32  # load commands follow the 64-bit Mach-O header, whose ncmds is at 16
    for _ in range(struct.unpack_from("<I", exe, 16)[0]):
        cmd, size = struct.unpack_from("<2I", exe, off)
        if cmd == 0x32:  # LC_BUILD_VERSION: cmd, cmdsize, platform, minos (X.Y.Z as xxxx.yy.zz)
            v = struct.unpack_from("<I", exe, off + 12)[0]
            return f"{v >> 16}_{v >> 8 & 0xff}"
        off += size
    sys.exit("mkwheels: no LC_BUILD_VERSION in a Darwin binary")

LIN = lambda a: lambda exe: f"manylinux_2_17_{a}.manylinux2014_{a}.musllinux_1_1_{a}"  # static binary: glibc and musl
MAC = lambda a: lambda exe: f"macosx_{macos_min(exe)}_{a}"
TAGS = {  # asset -> wheel platform tag
    "isworkday-Linux-x86_64": LIN("x86_64"), "isworkday-Linux-aarch64": LIN("aarch64"),
    "isworkday-Darwin-x86_64": MAC("x86_64"), "isworkday-Darwin-arm64": MAC("arm64"),
}
ROOT = pathlib.Path(__file__).resolve().parent.parent
LICENSES = {n: (ROOT / n).read_bytes() for n in ("LICENSE", "NOTICE")}  # PEP 639 license files
LICENSE_FIELDS = "".join(f"License-File: {n}\n" for n in LICENSES)
DESC = (ROOT / "README.md").read_text(encoding="utf-8").split("<!-- pypi: end -->")[0]  # PyPI page
version, assets, out = sys.argv[1], pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3])
out.mkdir(parents=True, exist_ok=True)
for asset, tag in TAGS.items():
    exe = (assets / asset).read_bytes()
    plat, di = tag(exe), f"{NAME}-{version}.dist-info"
    files = {  # path -> (bytes, unix mode)
        f"{NAME}-{version}.data/scripts/{NAME}": (exe, 0o755),
        f"{di}/METADATA": (f"Metadata-Version: 2.4\nName: {NAME}\nVersion: {version}\n"
                           "Summary: Command-line predicate: is DATE a statutory workday in REGION? One word, one exit code, official calendars that update themselves. Only cn (mainland China) so far.\n"
                           f"License-Expression: MIT\n{LICENSE_FIELDS}Requires-Python: >=3.8\n"
                           f"Description-Content-Type: text/markdown\n\n{DESC}".encode(), 0o644),
        **{f"{di}/licenses/{n}": (d, 0o644) for n, d in LICENSES.items()},
        f"{di}/WHEEL": (f"Wheel-Version: 1.0\nGenerator: mkwheels\nRoot-Is-Purelib: false\n".encode() + "".join(f"Tag: py3-none-{p}\n" for p in plat.split(".")).encode(), 0o644),
    }
    rec = "".join(f"{p},sha256={base64.urlsafe_b64encode(hashlib.sha256(d).digest()).rstrip(b'=').decode()},{len(d)}\n" for p, (d, _) in files.items())
    files[f"{di}/RECORD"] = ((rec + f"{di}/RECORD,,\n").encode(), 0o644)
    with zipfile.ZipFile(out / f"{NAME}-{version}-py3-none-{plat}.whl", "w", zipfile.ZIP_DEFLATED) as z:
        for p, (d, mode) in files.items():
            zi = zipfile.ZipInfo(p, (1980, 1, 1, 0, 0, 0)); zi.external_attr = (0o100000 | mode) << 16; zi.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(zi, d)
