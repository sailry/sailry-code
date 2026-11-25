import 'package:flutter/widgets.dart';

import 'package:sailry_mobile/features/resources/contributions.dart';
import 'package:sailry_mobile/features/resources/file_navigation.dart';
import 'package:sailry_mobile/features/resources/overview_cards.dart';
import 'package:sailry_mobile/l10n/strings.dart';
import 'package:sailry_mobile/runtime/session.dart';
import 'package:sailry_mobile/ui/kit.dart';

import '../files_page.dart';

const filesResource = ProjectResourceFeature(
  entry: _filesEntry,
  openDirectory: _openDirectory,
  links: [
    ProjectLinkContribution.target('file', _openFileLink),
    ProjectLinkContribution.target('directory', _openDirectoryLink),
  ],
);

ResourceEntry _filesEntry(BuildContext context, ProjectResourceView view) => (
  icon: 'folder',
  label: context.tr('files'),
  detail: null,
  onTap: () =>
      view.open(FilesPage(hostId: view.host.id, worktreeId: view.worktree)),
);

Future<void> _openFileLink(BuildContext context, ProjectResourceLink link) =>
    openResourceFile(
      context,
      link.host,
      link.worktree,
      link.data as String,
      documents: link.documents,
    );

Future<void> _openDirectoryLink(
  BuildContext context,
  ProjectResourceLink link,
) => _openDirectory(context, link.host, link.worktree, link.data as String);

Future<void> _openDirectory(
  BuildContext context,
  HostConnection host,
  String worktree,
  String directory,
) => pushPage<void>(
  context,
  FilesPage(hostId: host.id, worktreeId: worktree, initialDirectory: directory),
);
