import 'package:flutter_test/flutter_test.dart';
import 'package:sailry_code_mobile/assembly/code.dart';

void main() {
  test('Code mobile uses the unified Code release feed', () {
    expect(codeReleaseRepository, 'sailry/sailry-code');
    expect(codeMobileTagPrefix, 'v');
    final service = codeAssembly.createUpdates!();
    service.dispose();
  });
}
