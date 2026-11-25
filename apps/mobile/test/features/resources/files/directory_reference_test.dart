import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sailry_code_mobile/features/resources/files/contribution.dart';
import 'package:sailry_code_mobile/features/resources/files_page.dart';
import 'package:sailry_mobile/app.dart';
import 'package:sailry_mobile/assembly/pages.dart';
import 'package:sailry_mobile/features/conversations/live/references.dart';
import 'package:sailry_mobile/features/resources/resources_page.dart';
import 'package:sailry_mobile/l10n/strings.dart';
import 'package:sailry_mobile/runtime/session.dart';
import 'package:sailry_mobile/shell/assembly.dart';
import 'package:sailry_mobile/ui/package_assets.dart';

const _worktree = 'tree';

void main() {
  testWidgets('Files adds a project entry and opens directory references', (
    tester,
  ) async {
    final calls = <(String, Map<String, dynamic>?)>[];
    final host = HostConnection.test(
      id: 'entry/fixture',
      label: 'Fixture host',
      command: (kind, data) async {
        calls.add((kind, data));
        return {
          'data': kind == 'list_directory'
              ? {'entries': []}
              : <String, dynamic>{},
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
    final session = AppSession.test(hosts: [host]);
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
                  'target': {'kind': 'directory', 'data': 'src'},
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

    await tester.pumpWidget(const SizedBox.shrink());
    await session.close();
    session.dispose();
  });
}
