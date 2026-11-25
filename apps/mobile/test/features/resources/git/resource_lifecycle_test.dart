import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sailry_code_mobile/features/resources/git/contribution.dart';
import 'package:sailry_code_mobile/features/resources/git/overview_card.dart';
import 'package:sailry_mobile/app.dart';
import 'package:sailry_mobile/assembly/pages.dart';
import 'package:sailry_mobile/features/resources/resources_page.dart';
import 'package:sailry_mobile/runtime/session.dart';
import 'package:sailry_mobile/shell/assembly.dart';
import 'package:sailry_mobile/ui/package_assets.dart';

const _worktree = 'tree';

void main() {
  testWidgets(
    'Git work is scoped to its contribution and stops updating on pop',
    (tester) async {
      final calls = <String>[];
      final inspect = Completer<Map<String, dynamic>>();
      final host = HostConnection.test(
        id: 'entry/fixture',
        label: 'Fixture host',
        command: (kind, data) async {
          calls.add(kind);
          if (kind == 'inspect_git') {
            expect(data?['worktree'], _worktree);
            return inspect.future;
          }
          return {'data': {}};
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
        pages: [projectsPage],
        defaultRoute: projectsRoute,
        resources: const [gitResource],
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
      expect(find.byType(GitOverviewCard), findsOneWidget);
      expect(calls.where((kind) => kind == 'inspect_git'), hasLength(1));

      Navigator.of(tester.element(find.byType(ResourcesPage))).pop();
      await tester.pumpAndSettle();
      expect(find.byType(GitOverviewCard, skipOffstage: false), findsNothing);
      inspect.complete({
        'data': {'kind': 'repository', 'branch': 'main'},
      });
      await tester.pumpAndSettle();
      expect(calls, isNot(contains('list_git_branches')));
      expect(tester.takeException(), isNull);

      await tester.pumpWidget(const SizedBox.shrink());
      await session.close();
      session.dispose();
    },
  );
}
