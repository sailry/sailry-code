import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sailry_code_mobile/assembly/code.dart';
import 'package:sailry_mobile/features/conversations/live/project_bar.dart';
import 'package:sailry_mobile/features/resources/contributions.dart';
import 'package:sailry_code_mobile/features/resources/files_page.dart';
import 'package:sailry_code_mobile/features/resources/git/overview_card.dart';
import 'package:sailry_code_mobile/features/resources/git_page.dart';
import 'package:sailry_mobile/features/resources/projects_page.dart';
import 'package:sailry_mobile/features/resources/workspace.dart';
import 'package:sailry_mobile/l10n/strings.dart';
import 'package:sailry_mobile/runtime/session.dart';
import 'package:sailry_mobile/ui/theme.dart';

const scratchName = '00000000-0000-4000-8000-000000000001';
const scratch = {
  'id': 'scratch',
  'project': null,
  'path': '/fixture/workspaces/sessions/$scratchName',
};

HostConnection source({CommandHandler? command}) => HostConnection.test(
  id: 'host',
  label: 'Host',
  command: command,
  snapshot: {
    'projects': [
      {'id': 'project', 'name': 'User project', 'path': '/fixture/repository'},
    ],
    'worktrees': [
      scratch,
      {'id': 'orphan', 'project': 'removed', 'path': '/fixture/orphan'},
      {
        'id': 'linked',
        'project': 'project',
        'path': '/fixture/feature',
        'main': false,
      },
      {
        'id': 'main',
        'project': 'project',
        'path': '/fixture/repository',
        'main': true,
      },
    ],
  },
);

Widget screen(AppSession session, Widget child) => ProjectResourcesScope(
  features: codeAssembly.resources,
  child: SessionScope(
    session: session,
    child: MaterialApp(
      theme: SailryTheme.of(Brightness.light),
      locale: const Locale('en'),
      supportedLocales: AppLocalizations.supportedLocales,
      localizationsDelegates: AppLocalizations.localizationsDelegates,
      home: Scaffold(body: child),
    ),
  ),
);

Future<void> release(WidgetTester tester, AppSession session) async {
  await tester.pumpWidget(const SizedBox.shrink());
  await session.close();
  session.dispose();
}

void main() {
  testWidgets('project resources can open Files and Git on a linked worktree', (
    tester,
  ) async {
    final host = source(
      command: (kind, data) async => {
        'data': {
          if (kind == 'inspect_git') ...{
            'kind': 'repository',
            'branch': 'branch-${data!['worktree']}',
          },
        },
      },
    );
    final session = AppSession.test(hosts: [host]);
    session.selectProject(host.id, unassignedProject);
    await tester.pumpWidget(screen(session, const ProjectsPage()));
    await tester.tap(find.text('User project'));
    await tester.pumpAndSettle();
    expect(session.selectedProjectId, unassignedProject);
    expect(session.selectedWorktreeId, isNull);
    await tester.tap(find.byType(ProjectBar));
    await tester.pumpAndSettle();
    expect(find.text(scratchName), findsNothing);
    await tester.tap(find.text('feature'));
    await tester.pumpAndSettle();
    expect(session.selectedProjectId, unassignedProject);
    expect(session.selectedWorktreeId, isNull);
    await tester.tap(
      find.descendant(
        of: find.byType(GitOverviewCard),
        matching: find.text('branch-linked'),
      ),
    );
    await tester.pumpAndSettle();
    expect(tester.widget<GitPage>(find.byType(GitPage)).worktreeId, 'linked');
    Navigator.of(tester.element(find.byType(GitPage))).pop();
    await tester.pumpAndSettle();
    await tester.tap(find.text('Files'));
    await tester.pumpAndSettle();
    expect(
      tester.widget<FilesPage>(find.byType(FilesPage)).worktreeId,
      'linked',
    );
    await release(tester, session);
  });
}
