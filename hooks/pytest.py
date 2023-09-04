import subprocess
import sys


def main():
    result = subprocess.run(
        ["pytest", "api"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    sys.exit(result.returncode)


if __name__ == "__main__":
    main()
