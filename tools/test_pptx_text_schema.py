"""Offline official ECMA-376 checks for owned editable-word PPTX fixtures.

Pass the existing transitional XSD directory, then PPTX files. Uses only the
standard library and xmllint; never Keynope's reader or an Office runtime.
"""
import copy
import json
from pathlib import Path
import subprocess
import sys
import xml.etree.ElementTree as ET
import zipfile

A = 'http://schemas.openxmlformats.org/drawingml/2006/main'
P = 'http://schemas.openxmlformats.org/presentationml/2006/main'
MC = 'http://schemas.openxmlformats.org/markup-compatibility/2006'
schemas = Path(sys.argv[1])
checks = []

def validate(root, schema):
    return subprocess.run(['xmllint', '--nonet', '--noout', '--schema', str(schemas / schema), '-'],
                          input=ET.tostring(root), capture_output=True)

for filename in sys.argv[2:]:
    with zipfile.ZipFile(filename) as package:
        for name in package.namelist():
            if not name.endswith('.xml') or not name.startswith('ppt/'):
                continue
            data = package.read(name)
            assert len(data) <= 16 * 1024 * 1024
            root = ET.fromstring(data)
            # Perform MCE's ignorable-attribute processing before validating
            # the core schema. Do not silently discard extension elements.
            for node in root.iter():
                node.attrib.pop(f'{{{MC}}}Ignorable', None)
                assert not node.tag.startswith('{http://schemas.microsoft.com/office/powerpoint/')
            schema = 'dml-main.xsd' if root.tag in (f'{{{A}}}theme', f'{{{A}}}tblStyleLst') else 'pml.xsd'
            result = validate(root, schema)
            assert result.returncode == 0, f'{name}: {result.stderr.decode()}'
            runs = root.findall(f'.//{{{P}}}txBody/{{{A}}}p/{{{A}}}r') + root.findall(f'.//{{{A}}}txBody/{{{A}}}p/{{{A}}}r')
            if runs:
                # A real negative schema control, not a Keynope assertion:
                # explicit font sizes below one point must be rejected.
                pr = runs[0].find(f'{{{A}}}rPr')
                created = pr is None
                if created:
                    pr = ET.Element(f'{{{A}}}rPr')
                    runs[0].insert(0, pr)
                old_size = pr.get('sz')
                pr.set('sz', '0')
                assert validate(root, schema).returncode != 0, 'Invalid word font size passed schema'
                if old_size is None:
                    pr.attrib.pop('sz')
                else:
                    pr.set('sz', old_size)
                if created:
                    runs[0].remove(pr)
            for style in root.findall(f'./{{{A}}}tblStyle'):
                identity = style.get('styleId')
                style.set('styleId', 'not-a-guid')
                assert validate(root, schema).returncode != 0, 'Invalid table-style GUID passed schema'
                style.set('styleId', identity)
            custom = root.findall(f'.//{{{A}}}custGeom')
            gradients = root.findall(f'.//{{{A}}}rPr/{{{A}}}gradFill')
            if gradients:
                invalid = copy.deepcopy(root)
                stop = invalid.find(f'.//{{{A}}}rPr/{{{A}}}gradFill/{{{A}}}gsLst/{{{A}}}gs')
                stop.set('pos', '-1')
                assert validate(invalid, schema).returncode != 0, 'Negative character gradient stop passed schema'
            if custom:
                invalid = copy.deepcopy(root)
                curve = invalid.find(f'.//{{{A}}}custGeom/{{{A}}}pathLst/{{{A}}}path/{{{A}}}cubicBezTo')
                if curve is not None:
                    curve.remove(curve[-1])
                    assert validate(invalid, schema).returncode != 0, 'Two-point cubic passed schema'
                invalid = copy.deepcopy(root)
                invalid.find(f'.//{{{A}}}custGeom/{{{A}}}pathLst/{{{A}}}path').set('w', '-1')
                assert validate(invalid, schema).returncode != 0, 'Negative path width passed schema'
            attachments = root.findall(f'.//{{{P}}}cNvCxnSpPr/{{{A}}}stCxn') + root.findall(f'.//{{{P}}}cNvCxnSpPr/{{{A}}}endCxn')
            for connection in attachments:
                bodies = [shape for shape in root.findall(f'.//{{{P}}}sp')
                          if shape.find(f'{{{P}}}nvSpPr/{{{P}}}cNvPr').get('id') == connection.get('id')]
                assert len(bodies) == 1, 'Missing or ambiguous native connection target'
                geometry = bodies[0].find(f'{{{P}}}spPr/{{{A}}}custGeom')
                if geometry is not None:
                    assert int(connection.get('idx')) < len(geometry.findall(f'{{{A}}}cxnLst/{{{A}}}cxn')), 'Missing connection site'
                old_index = connection.get('idx')
                connection.set('idx', '-1')
                assert validate(root, schema).returncode != 0, 'Negative connection index passed schema'
                connection.set('idx', old_index)
            checks.append({'part': name, 'file': str(filename), 'runs': len(runs),
                           'invalidWordSizeRejected': bool(runs), 'characterGradients': len(gradients),
                           'invalidGradientStopRejected': bool(gradients), 'customPaths': len(custom), 'attachments': len(attachments)})
assert any(item['runs'] for item in checks), 'Fixture contains no editable text'
print(json.dumps({'passed': True, 'validator': 'libxml2 official ECMA-376 transitional', 'parts': checks}, indent=2))
