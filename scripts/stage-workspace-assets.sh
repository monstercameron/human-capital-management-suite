#!/usr/bin/env bash
# stage-workspace-assets.sh <source-dir> <destination-dir>
#
# Clear the destination directory and copy every regular file from the
# source directory into it. scripts/build.sh uses this to mirror the
# candidate source asset catalog into the embed staging directory before
# the bundle builder generates the asset manifest from that directory:
# the manifest can only describe staged files, while the runtime validator
# compares it against the whole embedded catalog, so a partially staged
# directory produces a manifest the server rejects on startup.
#
# This script deletes regular files, so it validates its arguments before
# clearing anything: both arguments are required and non-empty, the source
# must be an existing directory, and the destination must not be the
# filesystem root (or a drive root), must not be a top-level directory, and
# must neither equal, contain, nor be contained in the source. A missing
# destination is created only after its existing parent is canonicalized and
# checked. Rejected invocations leave both directories untouched.
set -eu

if [ "$#" -ne 2 ]; then
  echo "stage-workspace-assets: usage: stage-workspace-assets.sh <source-dir> <destination-dir>" >&2
  exit 2
fi
src_arg=$1
dst_arg=$2

if [ -z "$src_arg" ] || [ -z "$dst_arg" ]; then
  echo "stage-workspace-assets: source and destination must both be non-empty" >&2
  exit 2
fi
if [ ! -d "$src_arg" ]; then
  echo "stage-workspace-assets: source is not an existing directory: $src_arg" >&2
  exit 1
fi
# String-level refusals for the shapes that must never reach even the
# directory creation below; canonical checks follow for the rest.
case "$dst_arg" in
"" | "/" | "." | "..")
  echo "stage-workspace-assets: unsafe destination: $dst_arg" >&2
  exit 2
  ;;
esac

src_dir=$(cd -- "$src_arg" && pwd -P)

# Go's embed patterns reject symlinks. Fail before touching the destination so
# the staged catalog always corresponds to regular source files.
shopt -s nullglob dotglob
for asset in "$src_dir"/*; do
  if [ -L "$asset" ]; then
    echo "stage-workspace-assets: source catalog contains unsupported symlink: ${asset##*/}" >&2
    exit 2
  fi
done
shopt -u dotglob nullglob

# Resolve an existing destination through symlinks. For a new destination,
# resolve its parent first so containment checks run before mkdir can mutate
# the source tree.
dst_path=$dst_arg
while [ "$dst_path" != "/" ] && [ "${dst_path%/}" != "$dst_path" ]; do
  dst_path=${dst_path%/}
done
dst_base=${dst_path##*/}
dst_parent_arg=${dst_path%/*}
if [ "$dst_path" = "$dst_base" ]; then
  dst_parent_arg=.
elif [ -z "$dst_parent_arg" ]; then
  dst_parent_arg=/
fi
if [ -z "$dst_base" ] || [ "$dst_base" = "." ] || [ "$dst_base" = ".." ]; then
  echo "stage-workspace-assets: unsafe destination: $dst_arg" >&2
  exit 2
fi
if [ -d "$dst_path" ]; then
  dst_dir=$(cd -- "$dst_path" && pwd -P)
else
  if [ ! -d "$dst_parent_arg" ]; then
    echo "stage-workspace-assets: destination parent is not an existing directory: $dst_parent_arg" >&2
    exit 1
  fi
  dst_parent=$(cd -- "$dst_parent_arg" && pwd -P)
  dst_dir=$dst_parent/$dst_base
fi

# Canonical refusals: the filesystem root and drive roots, destinations so
# shallow they name a system directory, and any destination that equals,
# contains or sits inside the source (clearing it could delete the input).
case "$src_dir" in
"" | "/")
  echo "stage-workspace-assets: unsafe source: $src_arg" >&2
  exit 2
  ;;
esac
case "$dst_dir" in
"" | "/")
  echo "stage-workspace-assets: unsafe destination: $dst_arg" >&2
  exit 2
  ;;
esac
case "$dst_dir" in
/[A-Za-z] | /mnt/[A-Za-z] | /cygdrive/[A-Za-z])
  echo "stage-workspace-assets: unsafe destination (drive root): $dst_dir" >&2
  exit 2
  ;;
esac
drive_child=
case "$dst_dir" in
/[A-Za-z]/*)
  drive_child=${dst_dir#/?/}
  ;;
/mnt/[A-Za-z]/*)
  drive_child=${dst_dir#/mnt/?/}
  ;;
/cygdrive/[A-Za-z]/*)
  drive_child=${dst_dir#/cygdrive/?/}
  ;;
esac
if [ -n "$drive_child" ]; then
  case "$drive_child" in
  */*) ;;
  *)
    echo "stage-workspace-assets: unsafe destination (top-level directory): $dst_dir" >&2
    exit 2
    ;;
  esac
fi
case "$dst_dir" in
/*/*) ;;
*)
  echo "stage-workspace-assets: unsafe destination (top-level directory): $dst_dir" >&2
  exit 2
  ;;
esac
if [ "$src_dir" = "$dst_dir" ]; then
  echo "stage-workspace-assets: source and destination are the same directory: $src_dir" >&2
  exit 2
fi
case "$src_dir/" in
"$dst_dir/"*)
  echo "stage-workspace-assets: destination contains the source: $dst_dir" >&2
  exit 2
  ;;
esac
case "$dst_dir/" in
"$src_dir/"*)
  echo "stage-workspace-assets: destination is inside the source: $dst_dir" >&2
  exit 2
  ;;
esac

# build.sh creates .artifacts/embed before calling this helper. Standalone
# callers may create one final destination level, after all safety checks.
if [ ! -d "$dst_dir" ]; then
  mkdir -- "$dst_dir"
fi

# Clear regular files (including dotfiles such as an interrupted build's
# temporary manifest) and then stage the source catalog the same way.
# Directories and anything that is not a regular file or symlink are left
# alone: this script mirrors a flat asset catalog, nothing else.
shopt -s nullglob dotglob
for staged in "$dst_dir"/*; do
  if [ -f "$staged" ] || [ -L "$staged" ]; then
    rm -f -- "$staged"
  fi
done
for asset in "$src_dir"/*; do
  if [ -f "$asset" ] && [ ! -L "$asset" ]; then
    cp -- "$asset" "$dst_dir/${asset##*/}"
  fi
done
shopt -u dotglob nullglob
