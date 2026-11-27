import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sailry_code_mobile/assembly/code.dart';
import 'package:sailry_code_mobile/features/resources/files/contribution.dart';
import 'package:sailry_code_mobile/features/resources/git/contribution.dart';
import 'package:sailry_mobile/app.dart';
import 'package:sailry_mobile/assembly/pages.dart';
import 'package:sailry_mobile/features/conversations/live/draft_settings.dart';
import 'package:sailry_mobile/features/conversations/live/team.dart';
import 'package:sailry_mobile/features/resources/ports_contribution.dart';
import 'package:sailry_mobile/features/terminal/project_resource.dart';
import 'package:sailry_mobile/l10n/strings.dart';
import 'package:sailry_mobile/runtime/json.dart';
import 'package:sailry_mobile/runtime/session.dart';
import 'package:sailry_mobile/shell/assembly.dart';
import 'package:sailry_mobile/ui/package_assets.dart';

void main() {
  test('Code keeps its root routes and project resources in order', () {
    expect(codeAssembly.displayName, 'Sailry Code');
    expect(codeAssembly.defaultRoute, chatRoute);
    expect(codeAssembly.pages.map((page) => page.id), [
      chatRoute,
      hostsRoute,
      projectsRoute,
      settingsRoute,
    ]);
    expect(codeAssembly.resources, [
      gitResource,
      filesResource,
      terminalResource,
      portsResource,
    ]);
  });

  testWidgets('Code owns developer choices in the unchanged common draft', (
    tester,
  ) async {
    final session = AppSession.test(
      initialize: () async => fail('Demo opened the native Client'),
    );
    await session.pairPin('888888');
    final host = session.selectedHost!;
    final projects = objects(host.snapshot['projects']);
    final project = projects.first;
    final worktrees = objects(
      host.snapshot['worktrees'],
    ).where((worktree) => worktree['project'] == project['id']).toList();
    Map<String, dynamic>? changed;
    final assembly = MobileAssembly(
      defaultRoute: chatRoute,
      resources: codeAssembly.resources,
      draftWorkspace: codeAssembly.draftWorkspace,
      conversationConfiguration: codeAssembly.conversationConfiguration,
      pages: [
        MobilePage(
          id: chatRoute,
          icon: 'chat',
          label: 'chat',
          build: (_) => SingleChildScrollView(
            child: DraftSettings(
              host: host,
              projects: projects,
              worktrees: worktrees,
              project: project,
              worktree: worktrees.first,
              providers: const [],
              config: const {'mode': 'code', 'permission': 'ask'},
              team: TeamChoices(
                listenable: session,
                read: () => const SessionTeam({}, []),
                onTeam: (_) async {},
                onMember: (_, _) async {},
              ),
              onProject: (_) {},
              onWorktree: (_) {},
              onConfig: (config) => changed = config,
            ),
          ),
        ),
      ],
    );
    await tester.pumpWidget(
      SailryApp(
        assembly: assembly,
        session: session,
        assets: PackageAssetBundle('sailry_mobile'),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('draft-project')), findsOneWidget);
    expect(find.byKey(const ValueKey('draft-worktree')), findsOneWidget);
    expect(
      find.byKey(const ValueKey('configuration-permission')),
      findsOneWidget,
    );
    final mode = find.byKey(const ValueKey('configuration-mode'));
    expect(mode, findsOneWidget);
    final label = tester.element(mode).tr('conversationPlan');
    await tester.ensureVisible(mode);
    await tester.tap(mode);
    await tester.pumpAndSettle();
    await tester.tap(find.text(label));
    await tester.pumpAndSettle();
    expect(changed, {'mode': 'plan', 'permission': 'ask'});
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
    await session.close();
    session.dispose();
  });
}
