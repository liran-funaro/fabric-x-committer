#!/bin/bash

# Copyright IBM Corp All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0

mode=$1
project_path=$2

bin_file="${project_path}/bin/loadgen"
doc_file="${project_path}/docs/loadgen-artifacts.md"
config_file="${project_path}/cmd/config/samples/loadgen.yaml"

# Generate the artifacts to a temporary folder.
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT
export SC_LOADGEN_LOAD_PROFILE_POLICY_ARTIFACTS_PATH=${TMPDIR}
"${bin_file}" make-artifacts -c "${config_file}" >/dev/null 2>&1

# Fetch the tree output. Replace &nbsp with regular spaces.
cd "${TMPDIR}" || exit
TREE=$(tree . | sed 's/\xc2\xa0/ /g')

# Replace content between markers with the tree output wrapped in code blocks.
updated_doc=$(awk -v tree="$TREE" '
    /<!-- TREE MARKER START -->/ {
        print
        print "```"
        print tree
        print "```"
        skip=1
        next
    }
    /<!-- TREE MARKER END -->/ {
        skip=0
    }
    !skip
' < "${doc_file}")

# Check mode: verify that updated_doc matches existing doc_file
if [ "${mode}" = "check" ]; then
    if [ "${updated_doc}" = "$(cat "${doc_file}")" ]; then
        exit 0
    else
        echo "✗ Documentation is out of date. Run 'make generate-sample-tree' to update it."
        exit 1
    fi
fi

# Write the updated content back to the file
echo "${updated_doc}" > "${doc_file}"
