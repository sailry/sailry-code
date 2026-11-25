import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sailry_mobile/features/conversations/live/presentation.dart';
import 'package:sailry_mobile/features/resources/scope_picker.dart';
import 'package:sailry_mobile/l10n/strings.dart';
import 'package:sailry_mobile/shell/assembly.dart';
import 'package:sailry_mobile/ui/kit.dart';
import 'package:sailry_mobile/ui/toast.dart';

import 'draft_branches.dart';

class DraftWorkspace extends StatefulWidget {
  const DraftWorkspace({super.key, required this.workspace});

  final DraftWorkspaceContext workspace;

  @override
  State<DraftWorkspace> createState() => _DraftWorkspaceState();
}

class _DraftWorkspaceState extends State<DraftWorkspace> {
  late final _branches = DraftBranches(widget.workspace.host);

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void didUpdateWidget(DraftWorkspace oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.workspace.selected?['project'] !=
            widget.workspace.selected?['project'] ||
        oldWidget.workspace.selected == null &&
            widget.workspace.selected != null) {
      unawaited(_load());
    }
  }

  Future<void> _load() async {
    await _branches.load(widget.workspace.selected);
    final error = _branches.error;
    if (mounted && error != null) {
      showToast(context, failureLabel(error, translate: context.tr));
    }
  }

  Future<void> _choose() async {
    unawaited(_load());
    final workspace = widget.workspace;
    final choice = await showAppSheet<Map<String, dynamic>>(
      context,
      context.tr('gitBranches'),
      child: ListenableBuilder(
        listenable: _branches,
        builder: (context, _) => ScopePicker(
          rows: [
            for (final item in workspace.worktrees)
              ScopeRow(
                leading: const AppIcon('branch', size: scopeIconSize),
                title: _branches.label(item, context.tr('worktree')),
                titleLines: 2,
                selected: item['id'] == workspace.selected?['id'],
                onTap: () => Navigator.pop(context, item),
              ),
          ],
        ),
      ),
    );
    if (!mounted || choice == null) return;
    widget.workspace.onChanged(choice);
  }

  @override
  void dispose() {
    _branches.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: _branches,
    builder: (context, _) => ListTile(
      key: const ValueKey('draft-worktree'),
      leading: const AppIcon('branch', size: 20),
      title: Text(
        _branches.label(widget.workspace.selected, context.tr('worktree')),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      trailing: const AppIcon('chevron', size: 16),
      onTap: widget.workspace.enabled && widget.workspace.worktrees.isNotEmpty
          ? _choose
          : null,
    ),
  );
}
