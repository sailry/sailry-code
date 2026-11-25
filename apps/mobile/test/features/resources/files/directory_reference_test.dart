import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sailry_code_mobile/features/resources/files/contribution.dart';
import 'package:sailry_code_mobile/features/resources/files_page.dart';
import 'package:sailry_mobile/app.dart';
import 'package:sailry_mobile/assembly/pages.dart';
import 'package:sailry_mobile/features/conversations/live/references.dart';
import 'package:sailry_mobile/features/resources/file_page.dart';
import 'package:sailry_mobile/features/resources/resources_page.dart';
import 'package:sailry_mobile/l10n/strings.dart';
import 'package:sailry_mobile/runtime/session.dart';
import 'package:sailry_mobile/shell/assembly.dart';
import 'package:sailry_mobile/ui/package_assets.dart';

const _worktree = 'tree';

void main() {
  testWidgets('Files opens references on their captured Host', (tester) async {
    final calls = <(String, Map<String, dynamic>?)>[];
    final host = HostConnection.test(
      id: 'entry/fixture',
      label: 'Fixture host',
      command: (kind, data) async {
        calls.add((kind, data));
        return {
          'data': switch (kind) {
            'list_directory' => {'entries': []},
            'read_file' => {'text': 'Source host file', 'revision': 'r1'},
            _ => <String, dynamic>{},
          },
        };
      },
      snapshot: const {
        'projects': [
          {'id': 'project', 'name': 'Fixture project', 'path': '/fixture'},
        ],
        'worktrees': [
          {
            'id': _worktree,
            'project': 'project',
            'main': true,
            'path': '/fixture',
          },
        ],
      },
    );
    final otherCalls = <String>[];
    final other = HostConnection.test(
      id: 'other-entry/other-target',
      label: 'Other host',
      command: (kind, _) async {
        otherCalls.add(kind);
        return {};
      },
      snapshot: const {
        'worktrees': [
          {'id': _worktree, 'path': '/unrelated'},
        ],
      },
    );
    final session = AppSession.test(hosts: [host, other]);
    final assembly = MobileAssembly(
      defaultRoute: projectsRoute,
      resources: const [filesResource],
      pages: [
        MobilePage(
          id: chatRoute,
          icon: 'chat',
          label: 'chat',
          build: (_) => Center(
            child: MessageReferences(
              host: host,
              worktree: _worktree,
              references: const [
                {
                  'label': 'Source directory',
                  'target': {'kind': 'directory', 'data': '/fixture/src'},
                },
                {
                  'label': 'Source file',
                  'target': {'kind': 'file', 'data': '/fixture/src/main.txt'},
                },
              ],
            ),
          ),
        ),
        projectsPage,
      ],
    );
    await tester.pumpWidget(
      SailryApp(
        assets: PackageAssetBundle('sailry_mobile'),
        assembly: assembly,
        session: session,
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Fixture project'));
    await tester.pumpAndSettle();
    final resources = tester.element(find.byType(ResourcesPage));
    expect(find.text(resources.tr('files')), findsOneWidget);
    Navigator.of(resources).pop();
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const ValueKey('tab-$chatRoute')));
    await tester.pumpAndSettle();
    session.selectHost(other.id);
    await tester.pumpAndSettle();
    expect(session.selectedHost, same(other));

    await tester.tap(find.text('Source directory'));
    await tester.pumpAndSettle();
    final page = tester.widget<FilesPage>(find.byType(FilesPage));
    expect(
      (page.hostId, page.worktreeId, page.initialDirectory),
      (host.id, _worktree, 'src'),
    );
    expect(calls.singleWhere((call) => call.$1 == 'list_directory').$2, {
      'worktree': _worktree,
      'path': 'src',
      'after': null,
    });
    Navigator.of(tester.element(find.byType(FilesPage))).pop();
    await tester.pumpAndSettle();

    await tester.tap(find.text('Source file'));
    await tester.pumpAndSettle();
    final filePage = tester.widget<ResourceFilePage>(
      find.byType(ResourceFilePage),
    );
    expect(
      (filePage.host, filePage.worktree, filePage.path),
      (host, _worktree, 'src/main.txt'),
    );
    expect(filePage.document.draft?.text, 'Source host file');
    expect(calls.singleWhere((call) => call.$1 == 'read_file').$2, {
      'worktree': _worktree,
      'path': 'src/main.txt',
    });
    expect(otherCalls, isEmpty);

    await tester.pumpWidget(const SizedBox.shrink());
    await session.close();
    session.dispose();
  });
}
