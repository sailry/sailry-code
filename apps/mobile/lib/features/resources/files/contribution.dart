import 'package:flutter/widgets.dart';

import 'package:sailry_mobile/l10n/strings.dart';
import 'package:sailry_mobile/runtime/session.dart';
import 'package:sailry_mobile/ui/kit.dart';
import 'package:sailry_mobile/features/resources/contributions.dart';
import '../files_page.dart';
import 'package:sailry_mobile/features/resources/overview_cards.dart';

const filesResource = ProjectResourceFeature(
  entry: _filesEntry,
  openDirectory: _openDirectory,
);

ResourceEntry _filesEntry(BuildContext context, ProjectResourceView view) => (
  icon: 'folder',
  label: context.tr('files'),
  detail: null,
  onTap: () =>
      view.open(FilesPage(hostId: view.host.id, worktreeId: view.worktree)),
);

Future<void> _openDirectory(
  BuildContext context,
  HostConnection host,
  String worktree,
  String directory,
) => pushPage<void>(
  context,
  FilesPage(hostId: host.id, worktreeId: worktree, initialDirectory: directory),
);
