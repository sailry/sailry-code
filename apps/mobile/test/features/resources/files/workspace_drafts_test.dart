import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sailry_code_mobile/features/resources/files_page.dart';
import 'package:sailry_mobile/features/resources/file_page.dart';
import 'package:sailry_mobile/runtime/session.dart';
import 'package:sailry_mobile/ui/package_assets.dart';

void main() {
  testWidgets('Switching worktrees preserves drafts and pending requests', (
    tester,
  ) async {
    final host = HostConnection.test(
      id: 'fixture',
      label: 'Fixture host',
      snapshot: const {
        'projects': [
          {'id': 'project', 'path': '/project'},
        ],
        'worktrees': [
          {'id': 'first', 'project': 'project', 'path': '/first'},
          {'id': 'second', 'project': 'project', 'path': '/second'},
        ],
      },
      command: (kind, data) async => {
        'data': switch (kind) {
          'list_directory' => {
            'entries': [
              {'name': 'notes.txt', 'kind': 'file'},
            ],
          },
          'read_file' => {'text': data?['worktree'], 'revision': 'r1'},
          _ => throw StateError('Unexpected command $kind'),
        },
      },
    );
    final session = AppSession.test(hosts: [host])
      ..selectWorkspace(host.id, 'first');
    await tester.pumpWidget(
      DefaultAssetBundle(
        bundle: PackageAssetBundle('sailry_mobile'),
        child: SessionScope(
          session: session,
          child: const MaterialApp(home: FilesPage()),
        ),
      ),
    );
    await tester.pumpAndSettle();
    Future<FileDocument> open() async {
      await tester.tap(find.text('notes.txt'));
      await tester.pumpAndSettle();
      return tester
          .widget<ResourceFilePage>(find.byType(ResourceFilePage))
          .document;
    }

    Future<void> back() async {
      Navigator.of(tester.element(find.byType(ResourceFilePage))).pop();
      await tester.pumpAndSettle();
    }

    final first = await open();
    first.draft!.text = 'Unpublished edit';
    await back();
    session.selectWorkspace(host.id, 'second');
    await tester.pumpAndSettle();
    final second = await open();
    expect(second.draft!.text, 'second');
    second.draft!.pending = 'uncertain-save';
    await back();

    session.selectWorkspace(host.id, 'first');
    await tester.pumpAndSettle();
    expect((await open()).draft!.text, 'Unpublished edit');
    await back();
    session.selectWorkspace(host.id, 'second');
    await tester.pumpAndSettle();
    expect((await open()).draft!.pending, 'uncertain-save');

    await tester.pumpWidget(const SizedBox.shrink());
    await session.close();
    session.dispose();
  });
}
