"""Offline independent ECMA-376 checks for owned table PPTX fixtures.

Uses Python's standard library and system xmllint, not Keynope's reader. Pass
the existing official transitional XSD directory followed by PPTX filenames.
This optional test tool is not a runtime dependency or an Office renderer.
"""
import json
from pathlib import Path
import subprocess
import sys
import xml.etree.ElementTree as ET
import zipfile

P = 'http://schemas.openxmlformats.org/presentationml/2006/main'
A = 'http://schemas.openxmlformats.org/drawingml/2006/main'
MC = 'http://schemas.openxmlformats.org/markup-compatibility/2006'
schema = Path(sys.argv[1]) / 'pml.xsd'
checks = []
for filename in sys.argv[2:]:
    with zipfile.ZipFile(filename) as package:
        for name in package.namelist():
            if name != 'ppt/presentation.xml' and not (name.startswith('ppt/slides/slide') and name.endswith('.xml')):
                continue
            data = package.read(name)
            assert len(data) <= 16 * 1024 * 1024
            root = ET.fromstring(data)
            # MCE's Ignorable attribute is processed before validating the
            # core transitional schema. These fixtures require no P14 nodes;
            # reject rather than drop any non-core authored table extensions.
            for node in root.iter():
                node.attrib.pop(f'{{{MC}}}Ignorable', None)
                assert not node.tag.startswith('{http://schemas.microsoft.com/office/powerpoint/'), 'Use the timing schema tool for extended transitions'
            result = subprocess.run(['xmllint', '--nonet', '--noout', '--schema', str(schema), '-'], input=ET.tostring(root), capture_output=True)
            assert result.returncode == 0, result.stderr.decode()
            tables = root.findall(f'.//{{{A}}}tbl')
            for table in tables:
                columns = table.findall(f'{{{A}}}tblGrid/{{{A}}}gridCol')
                rows = table.findall(f'{{{A}}}tr')
                assert columns and rows
                assert all(len(row.findall(f'{{{A}}}tc')) == len(columns) for row in rows)
                for cell in table.findall(f'{{{A}}}tr/{{{A}}}tc'):
                    pr = cell.find(f'{{{A}}}tcPr')
                    assert pr is not None
                    for side in ('lnL', 'lnR', 'lnT', 'lnB'):
                        line = pr.find(f'{{{A}}}{side}')
                        assert line is not None and int(line.get('w')) >= 0
            if tables:
                # Prove the independent schema validator actually checks the
                # table stroke type; our own integer check is not the proof.
                line = tables[0].find(f'{{{A}}}tr/{{{A}}}tc/{{{A}}}tcPr/{{{A}}}lnT')
                width = line.get('w')
                line.set('w', '-1')
                rejected = subprocess.run(['xmllint', '--nonet', '--noout', '--schema', str(schema), '-'], input=ET.tostring(root), capture_output=True)
                assert rejected.returncode != 0, 'Schema failed to reject negative table stroke'
                line.set('w', width)
            checks.append({'file': str(filename), 'part': name, 'tables': len(tables), 'negativeStrokeRejected': bool(tables)})
print(json.dumps({'passed': True, 'validator': 'libxml2 official ECMA-376 transitional', 'parts': checks}, indent=2))
