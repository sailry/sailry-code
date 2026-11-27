import 'package:flutter/widgets.dart';
import 'package:sailry_mobile/assembly/pages.dart';
import 'package:sailry_mobile/features/resources/ports_contribution.dart';
import 'package:sailry_mobile/features/terminal/project_resource.dart';
import 'package:sailry_mobile/features/updates/service.dart';
import 'package:sailry_mobile/shell/assembly.dart';

import '../features/conversations/live/draft_workspace.dart';
import '../features/conversations/live/mode_picker.dart';
import '../features/resources/files/contribution.dart';
import '../features/resources/git/contribution.dart';

const codeReleaseRepository = 'sailry/sailry-code';
const codeMobileTagPrefix = 'v';

final codeAssembly = MobileAssembly(
  displayName: 'Sailry Code',
  pages: harnessPages,
  defaultRoute: chatRoute,
  resources: const [
    gitResource,
    filesResource,
    terminalResource,
    portsResource,
  ],
  draftWorkspace: (workspace) =>
      DraftWorkspace(key: ObjectKey(workspace.host), workspace: workspace),
  conversationConfiguration: (configuration) => ModePicker(
    key: ObjectKey(configuration.host),
    configuration: configuration,
  ),
  createUpdates: () => AppUpdates(
    repository: codeReleaseRepository,
    tagPrefix: codeMobileTagPrefix,
    requireBuildNumber: true,
  ),
);
