#!/bin/sh
set -eu

mobile=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
cd "$mobile"
flutter pub get
dart run tool/prepare.dart
