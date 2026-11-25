import 'package:flutter_test/flutter_test.dart';
import 'package:sailry_code_mobile/features/resources/git/model.dart';

Map<String, dynamic> _entry(
  String path, {
  String? staged,
  String? unstaged,
  bool untracked = false,
  bool conflicted = false,
}) => {
  'path': path,
  'staged': staged,
  'unstaged': unstaged,
  'untracked': untracked,
  'conflicted': conflicted,
  'diff': {'additions': 5, 'deletions': 2},
  'staged_diff': {'additions': 3, 'deletions': 0},
  'unstaged_diff': {'additions': 2, 'deletions': 2},
};

void main() {
  group('changeStatus', () {
    test("maps staged unstaged untracked and conflicted statuses", () {
      for (final (caseName, run) in <(String, void Function(String))>[
        (
          "maps each side to its letter",
          (caseName) {
            final entry = _entry('a', staged: 'added', unstaged: 'modified');
            expect(
              changeStatus(entry, staged: true),
              ChangeStatus.added,
              reason: caseName,
            );
            expect(
              changeStatus(entry, staged: false),
              ChangeStatus.modified,
              reason: caseName,
            );
            expect(
              changeStatus(
                _entry('b', unstaged: 'deleted'),
                staged: false,
              ).letter,
              'D',
              reason: caseName,
            );
          },
        ),
        (
          "marks untracked and conflicted entries",
          (caseName) {
            expect(
              changeStatus(_entry('a', untracked: true), staged: false),
              ChangeStatus.untracked,
              reason: caseName,
            );
            expect(
              changeStatus(
                _entry('b', staged: 'modified', conflicted: true),
                staged: true,
              ),
              ChangeStatus.conflicted,
              reason: caseName,
            );
          },
        ),
      ]) {
        run(caseName);
      }
    });
  });

  group('groupChanges', () {
    test('lists partially staged entries in both groups', () {
      final groups = groupChanges([
        _entry('both', staged: 'modified', unstaged: 'modified'),
        _entry('index', staged: 'added'),
        _entry('new', untracked: true),
        _entry('conflict', conflicted: true),
      ]);
      expect(groups.unstaged.map((entry) => entry['path']), [
        'both',
        'new',
        'conflict',
      ]);
      expect(groups.staged.map((entry) => entry['path']), ['both', 'index']);
    });
  });

  group('lineStats', () {
    test('reads the requested side and sums totals', () {
      final entry = _entry('a', staged: 'modified');
      expect(lineStats(entry), (additions: 5, deletions: 2));
      expect(lineStats(entry, staged: true), (additions: 3, deletions: 0));
      expect(lineStats(entry, staged: false), (additions: 2, deletions: 2));
      expect(totalStats([entry, entry]), (additions: 10, deletions: 4));
      expect(lineStats({'path': 'b'}), (additions: 0, deletions: 0));
    });
  });

  group('discardKind', () {
    test('restores tracked files and trashes untracked ones', () {
      expect(
        discardKind(_entry('a', unstaged: 'modified')),
        DiscardKind.restore,
      );
      expect(discardKind(_entry('b', untracked: true)), DiscardKind.trash);
      expect(discardKind(_entry('c', staged: 'added')), DiscardKind.none);
    });
  });

  group('commitOptions', () {
    test('maps choices onto Node fields', () {
      expect(commitOptions(), containsPair('after', 'none'));
      final options = commitOptions(skipHooks: true, push: true);
      expect(options['skip_hooks'], isTrue);
      expect(options['after'], 'push');
      expect(options['signoff'], isFalse);
    });
  });

  group('references', () {
    test("resolves local and remote reference names", () {
      for (final (caseName, run) in <(String, void Function(String))>[
        (
          "strip namespaces and match the current branch",
          (caseName) {
            expect(referenceLabel('heads/main'), 'main', reason: caseName);
            expect(
              referenceLabel('remotes/origin/main'),
              'origin/main',
              reason: caseName,
            );
            expect(referenceLabel('tags/v1'), 'v1', reason: caseName);
            expect(referenceLabel('stash'), 'stash', reason: caseName);
            expect(
              isCurrentReference('heads/main', 'main'),
              isTrue,
              reason: caseName,
            );
            expect(
              isCurrentReference('remotes/origin/main', 'main'),
              isFalse,
              reason: caseName,
            );
            expect(
              isCurrentReference('heads/main', null),
              isFalse,
              reason: caseName,
            );
            expect(shortHash('0123456789abcdef'), '0123456', reason: caseName);
          },
        ),
        (
          "split local and remote branches",
          (caseName) {
            final branches = splitBranches([
              {'name': 'origin/main', 'remote': true},
              {'name': 'main', 'remote': false},
            ]);
            expect(branches.local.single['name'], 'main', reason: caseName);
            expect(
              branches.remote.single['name'],
              'origin/main',
              reason: caseName,
            );
          },
        ),
      ]) {
        run(caseName);
      }
    });
  });
}
