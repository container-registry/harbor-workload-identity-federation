#!/bin/sh
#
# Node side of scripts/verify-node-install.sh: prints the command line of the
# process that is running kubelet on this node, or NO_KUBELET_PROCESS.
#
# Runs inside a debug pod chrooted into the host, so it stays POSIX sh and
# uses nothing beyond what a busybox image has. It takes the string to print
# when it finds no kubelet as $1, so the caller owns both ends of that word.
#
# kubelet is a separate process on most distributions, and embedded in the
# rke2 and k3s supervisors or in microk8s kubelite on others. Matching on the
# literal string "kubelet" with pgrep would also match this very snippet,
# whose own command line contains it, so the shell's own PID is excluded, as
# is anything that looks like the debug pod's own wrapper.

none=${1:?the string to print when no kubelet process is running}

self=$$
for entry in /proc/[0-9]*; do
  pid=${entry#/proc/}
  case "$pid" in
    *[!0-9]*) continue ;;
  esac
  [ "$pid" = "$self" ] && continue
  [ -r "/proc/$pid/cmdline" ] || continue
  cmd=$(tr "\0" " " < "/proc/$pid/cmdline")
  case "$cmd" in
    */bin/sh\ -c*|*chroot\ /host*) continue ;;
  esac
  case "$cmd" in
    */kubelet\ *|*/kubelet|*/kubelite\ *|*/rke2\ server*|*/rke2\ agent*|*/k3s\ server*|*/k3s\ agent*)
      echo "$cmd"
      exit 0
      ;;
  esac
done
echo "$none"
