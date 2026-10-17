# Postmortem: eu-north degradation, 2025-09-14

For 3 hours, publishing to eu-north returned errors for 12% of requests because a storage node ran out of disk. No acknowledged messages were lost. We added disk-pressure alerts and automatic node replacement.
