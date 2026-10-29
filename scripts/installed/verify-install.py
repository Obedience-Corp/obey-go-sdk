"""Validate public Festival receipts before running the SDK consumer."""
import hashlib
import json
import os
from pathlib import Path

if os.environ.get('OBEY_SDK_TEST_CONTAINER') != '1' or not Path('/.dockerenv').is_file():
    raise SystemExit('Disposable Docker fixture required')

evidence = Path('/evidence')
installation = json.loads((evidence / 'install.json').read_text())
assert installation['ok'] is True, installation
packages = json.loads((evidence / 'packages.json').read_text())['data']['packages']
receipt = next(p for p in packages if p['package_id'] == 'obedience-corp/obey')
assert receipt['source'] == 'official-obey', receipt
assert receipt['channel'] == 'stable', receipt
markets = json.loads((evidence / 'marketplaces.json').read_text())['data']
official = next(m for m in markets if m['name'] == 'official-obey')
assert official['verified'] is True, official
assert official['url'] == 'https://github.com/Obedience-Corp/marketplace.git', official
binary = Path('/demo/.obey/installer/bin/obey')
assert (evidence / 'obey-path.txt').read_text().strip() == str(binary)
assert binary.is_file() and os.access(binary, os.X_OK)
assert str(binary) in receipt['files'], receipt['files']
summary = {
    'package': receipt['package_id'], 'version': receipt['version'],
    'channel': receipt['channel'], 'source': receipt['source'],
    'marketplace_verified': True, 'marketplace_commit': official['commit'],
    'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
}
(evidence / 'installed-release.json').write_text(json.dumps(summary, indent=2) + '\n')
print('Festival installed Obey ' + receipt['version'] + ' from verified official-obey (stable).')
