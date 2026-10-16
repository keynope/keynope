"""Independently validate owned PPTX slides using official ECMA-376 schemas.

Pass the extracted OfficeOpenXML-XMLSchema-Transitional directory and PPTX
fixtures. This is optional acceptance tooling, not a runtime dependency.
"""
import json
import subprocess
import sys
import xml.etree.ElementTree as ET
import zipfile
from pathlib import Path

schemas = Path(sys.argv[1])
schema = schemas / "pml.xsd"


def validate(root):
    return subprocess.run(
        ["xmllint", "--nonet", "--noout", "--schema", str(schema), "-"],
        input=ET.tostring(root), capture_output=True,
    )


P = "http://schemas.openxmlformats.org/presentationml/2006/main"
MC = "http://schemas.openxmlformats.org/markup-compatibility/2006"
P14 = "http://schemas.microsoft.com/office/powerpoint/2010/main"
checks = []
negative_checks = 0
for filename in sys.argv[2:]:
    with zipfile.ZipFile(filename) as package:
        for name in package.namelist():
            if name != "ppt/presentation.xml" and not (name.startswith("ppt/slides/slide") and name.endswith(".xml")):
                continue
            data = package.read(name)
            assert len(data) <= 16 * 1024 * 1024, "Oversized fixture XML"
            root = ET.fromstring(data)
            if root.tag == f"{{{P}}}sld":
                shape_ids = [node.get("id") for node in root.findall(f".//{{{P}}}cSld//{{{P}}}cNvPr")]
                assert len(shape_ids) == len(set(shape_ids)), "Repeated shape ID"
                time_nodes = root.findall(f".//{{{P}}}cTn")
                time_ids = [node.get("id") for node in time_nodes]
                assert len(time_ids) == len(set(time_ids)), "Repeated time node ID"
                for target in root.findall(f".//{{{P}}}spTgt"):
                    assert target.get("spid") in shape_ids, "Dangling animation shape ID"
                for reference in root.findall(f".//{{{P}}}tn"):
                    assert reference.get("val") in time_ids, "Dangling timing dependency"
                for build in root.findall(f".//{{{P}}}bldLst/*"):
                    assert build.get("spid") in shape_ids, "Dangling build-list ID"
                for transition in root.findall(f"{{{P}}}transition"):
                    duration = transition.get(f"{{{P14}}}dur")
                    if duration is not None:
                        assert 1 <= int(duration) <= 60000
            # MCE processing removes ignorable Office-2010 attributes before
            # applying the transitional core schema. Assert durations above
            # rather than silently ignoring the extension's meaning.
            for node in root.iter():
                for attribute in list(node.attrib):
                    if any(attribute.startswith("{" + ns + "}") for ns in (MC, P14)):
                        del node.attrib[attribute]
            result = validate(root)
            assert result.returncode == 0, result.stderr.decode()
            # Prove that the independent validator is actually enforcing the
            # timing schema, rather than only accepting well-formed XML.
            for timing in root.findall(f".//{{{P}}}cTn")[:1]:
                previous = timing.get("nodeType")
                timing.set("nodeType", "invalid-node-type")
                assert validate(root).returncode != 0, "Invalid timing type passed schema"
                if previous is None:
                    del timing.attrib["nodeType"]
                else:
                    timing.set("nodeType", previous)
                negative_checks += 1
            checks.append(f"{Path(filename).name}:{name}")
assert negative_checks, "Fixtures did not exercise timing validation"
print(json.dumps({"passed": True, "schema": "ECMA-376 transitional", "parts": checks,
                  "invalidTimingTypesRejected": negative_checks}, indent=2))
