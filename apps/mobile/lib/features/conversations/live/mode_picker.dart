import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sailry_mobile/features/conversations/live/choice_row.dart';
import 'package:sailry_mobile/features/conversations/live/presentation.dart'
    show failureLabel;
import 'package:sailry_mobile/l10n/strings.dart';
import 'package:sailry_mobile/shell/assembly.dart';
import 'package:sailry_mobile/ui/kit.dart';
import 'package:sailry_mobile/ui/list_group.dart';
import 'package:sailry_mobile/ui/theme.dart';
import 'package:sailry_mobile/ui/toast.dart';

import 'modes.dart';

class ModePicker extends StatefulWidget {
  const ModePicker({super.key, required this.configuration});

  final ConversationConfigurationContext configuration;

  @override
  State<ModePicker> createState() => _ModePickerState();
}

class _ModePickerState extends State<ModePicker> {
  late ConversationModes _modes;
  Object? _reported;

  @override
  void initState() {
    super.initState();
    _observe();
  }

  void _observe() {
    _reported = null;
    _modes = ConversationModes(widget.configuration.host)
      ..addListener(_changed);
  }

  void _changed() {
    final failure = _modes.error;
    if (failure != null && !identical(failure, _reported)) {
      _reported = failure;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && identical(failure, _modes.error)) {
          showToast(context, failureLabel(failure, translate: context.tr));
        }
      });
    }
    if (mounted) setState(() {});
  }

  @override
  void didUpdateWidget(ModePicker oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.configuration.host != widget.configuration.host) {
      _modes.removeListener(_changed);
      _modes.dispose();
      _observe();
    }
  }

  @override
  void dispose() {
    _modes.removeListener(_changed);
    _modes.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final config = widget.configuration.config;
    final selectedMode = _modes.selected(config);
    List<(String, String, bool)> modeChoices() => [
      for (final (value, label) in const [
        ('code', 'conversationCode'),
        ('plan', 'conversationPlan'),
        ('flagship', 'conversationFlagship'),
      ])
        (value, context.tr(label), true),
      for (final mode in _modes.choices(widget.configuration.config))
        (mode.key, mode.label(Localizations.localeOf(context)), mode.enabled),
    ];
    final current = modeChoices()
        .where((mode) => mode.$1 == selectedMode)
        .firstOrNull;
    // Plugin packages can add modes, so a list scales where segments would not.
    final mode = ListGroupRow(
      key: const ValueKey('configuration-mode'),
      title: context.tr('conversationMode'),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            current?.$2 ?? '',
            style: TextStyle(
              fontSize: TypeScale.body,
              color: colors.onSurfaceVariant,
            ),
          ),
          const SizedBox(width: 6),
          AppIcon('chevron', size: 14, color: colors.onSurfaceVariant),
        ],
      ),
      onTap: !widget.configuration.enabled
          ? null
          : () async {
              if (_modes.error != null) unawaited(_modes.reload());
              final key = await showAppSheet<String>(
                context,
                context.tr('conversationMode'),
                child: ListenableBuilder(
                  listenable: _modes,
                  builder: (sheet, _) => Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      for (final (value, label, enabled) in modeChoices())
                        Opacity(
                          opacity:
                              enabled &&
                                  _modes.canSelect(widget.configuration.config)
                              ? 1
                              : .45,
                          child: ChoiceRow(
                            label: label,
                            selected:
                                value ==
                                _modes.selected(widget.configuration.config),
                            onTap:
                                enabled &&
                                    _modes.canSelect(
                                      widget.configuration.config,
                                    )
                                ? () => Navigator.pop(sheet, value)
                                : () {},
                          ),
                        ),
                    ],
                  ),
                ),
              );
              if (!mounted ||
                  key == null ||
                  key == _modes.selected(widget.configuration.config)) {
                return;
              }
              final next = _modes.select(widget.configuration.config, key);
              if (next != null) widget.configuration.onChanged(next);
            },
    );
    return ListGroup(children: [mode]);
  }
}
