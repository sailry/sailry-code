import 'dart:convert';
import 'dart:io';

void main() {
  final config = File('.dart_tool/package_config.json').absolute;
  final packages =
      (jsonDecode(config.readAsStringSync()) as Map)['packages']
          as List<dynamic>;
  final mobile = packages.cast<Map<String, dynamic>>().singleWhere(
    (package) => package['name'] == 'sailry_mobile',
  );
  final packageRoot = Directory.fromUri(
    config.uri.resolve(mobile['rootUri'] as String),
  );
  final root = packageRoot.parent.parent;
  if (!File('${root.path}/cmd/sailry-mobile/build.sh').existsSync()) {
    throw StateError('Resolved sailry_mobile has no Go Client/Link builder');
  }
  final link = Link('.dart_tool/harness');
  if (link.existsSync()) link.deleteSync();
  link.createSync(root.path);
}
