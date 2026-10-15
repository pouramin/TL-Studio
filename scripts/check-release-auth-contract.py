"""Fail PR validation when release smoke startup loses its launch credential."""
from pathlib import Path
source = Path('.github/workflows/release.yml').read_text()
start = source.index('      - name: Native product API contract test')
end = source.index('      - name: Package', start)
step = source[start:end]
assert step.index('export TL_STUDIO_LAUNCH_KEY=') < step.index('python3 scripts/check-native-product-contract.py')
assert 'test -n "$TL_STUDIO_LAUNCH_KEY"' in step
assert '::add-mask::$TL_STUDIO_LAUNCH_KEY' in step
print('Release launch authentication contract: PASS')
