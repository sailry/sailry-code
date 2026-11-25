import 'package:flutter_test/flutter_test.dart';
import 'package:sailry_code_mobile/assembly/code.dart';

void main() {
  test('Code mobile owns a release track independent of Desktop', () {
    expect(codeReleaseRepository, 'sailry/sailry-code');
    expect(codeMobileTagPrefix, 'mobile-v');
    final service = codeAssembly.createUpdates!();
    service.dispose();
  });
}
