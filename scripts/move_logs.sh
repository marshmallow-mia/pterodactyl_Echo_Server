#!/usr/bin/env bash
#
# EchoVR GameServer Log Organization Script
#
# This script will recursively search for logs within the "./old" folder and move them to "./archive".
# The "./archive" folder is organized by year and year-month folders, parsed from the filenames.
# The logs will also be renamed to include some additional information with easier formatting.


# CONSTANTS
search_dir="/opt/ready-at-dawn-echo-arena/logs"
new_dir_root="/opt/ready-at-dawn-echo-arena/logs"
archive_dir=archive
old_dir_name=old
readarray -t container_ids < <(docker ps --format '{{.Names}}')

# Loop "old" directories
#for old_dir in $(find ${search_dir} -type d | grep "${old_dir_name}" | grep -v "${archive_dir}" | sort -r); do

for container_id in ${container_ids[@]}
do
    fullLogPath="$search_dir/$container_id/old"
    #echo $fullLogPath

    # Loop "*.log" files from "old" directory
    for old_filename in $(ls -1 "${fullLogPath}" | grep '.log$')
    do
        old_path=${fullLogPath}/${old_filename}


        # Parse log filename
        #   Example: "[r14(server)]-[10-31-2023]_[23-59-59]_123.log"
        log_ver=${old_filename:1:3}
        log_mode=${old_filename:5:6}
        year=${old_filename:21:4}
        month=${old_filename:15:2}
        day=${old_filename:18:2}
        hour=${old_filename:28:2}
        minute=${old_filename:31:2}
        second=${old_filename:34:2}
        file_basename=${old_filename%%.log}
        process_id=${file_basename##*_}

        # Parse file for ip and port
        #   Example: [10-31-2024] [23:59:59]: Dedicated: registered as 0x0123456789ABCDEF at [192.168.0.2/1.1.1.1:1234]
        ip_address="0.0.0.0"
        port="0000"
        registered_line="$(grep -m1 ' Dedicated: registered as ' ${old_path})"
        if [[ ! "$registered_line" == "" ]]; then
            line_tail="${registered_line##*/}"
            connection="${line_tail%%]*}"
            ip_address="${connection%%:*}"
            port="${connection##*:}"
        fi  

        # Build new filename
        #   Example: 2023-10-31_23-59-59_r14_server_12345_123__1.1.1.1_1234.log
        new_filename="${year}-${month}-${day}_${hour}-${minute}-${second}_${log_ver}_${log_mode}_${container_id}_${process_id}_${ip_address}_${port}.log"


        # Move file
        new_dir=${new_dir_root}/${archive_dir}/${year}/${year}-${month}
        new_path=${new_dir}/${new_filename}
        echo "$old_filename: $old_path --> $new_path"
        #echo ""
        mkdir -p "${new_dir}"
        mv -n "${old_path}" "${new_path}"
    done
