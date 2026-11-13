import 'package:duanju_app/ui/player/player_status.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('network speed switches to MB/s above one megabyte', () {
    expect(formatNetworkSpeed(0), '0 KB/s');
    expect(formatNetworkSpeed(512 * 1024), '512 KB/s');
    expect(formatNetworkSpeed(1024 * 1024), '1.0 MB/s');
    expect(formatNetworkSpeed((3.5 * 1024 * 1024).toInt()), '3.5 MB/s');
  });
}
