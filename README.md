file\_reconstructor encodes to or decodes from a plaintext redundant format
which can be highly resistant to bit, byte, and burst errors, but not erasure
errors.

*NOTE: This utility lacks polish, but does work correctly. I will add polish
later if I have time.*

BASICS
------

In encode mode, stdin is repeatedly written to stdout after a fixed-length
header (send stdout to a block device, so the write continues until it runs out
of space).

During decode, that redundancy is used to repair any bit errors and recover the
original stream.

The length of the original stream is recorded redundantly in a header at the
beginning of the stream. If the length information becomes too heavily damaged,
the file will not be decoded correctly.

(it should be possible to determine the length of a stream programmatically
without the header, but this is not implemented yet. So for now, be wary of
burst errors which may obliterate the entire length header rendering it
unrecoverable.)

Note also, that a *single* erasure will render this coding useless. This should
NOT be used with devices which may erase bits or bytes (for example, tapes),
rather than simply flipping or deleting them.

file\_reconstructor DOES NOT checksum your data. If there is insufficient
redundancy to correct some errors, those errors will be present in the output.
You should probably compress the input data with something like bzip2 so you
get a checksum for free.

EXAMPLE
-------

TODO: for these examples to work, we need to add a flag that lets you change
the number of copies. Probably with warnings that it's not to be used in
production.

Here's a simple example which uses a program to simulate the addition of bit
errors to a stream:

> % echo "fizzbuzz" | file\_reconstructor | add\_bit\_errors 0.01 | xxd

After add\_bit\_errors, we have the following data (length header omitted):

> 0000000: 6669 7e7a 6275 7a7a 6669 7a5a 6275 7a7a  fi~zbuzzfizZbuzz
> 0000010: 6669 7a7a 6275 7a7a 6669 7a7a 6275 7a7a  fizzbuzzfizzbuzz
> 0000020: 6669 7a6a 6275 7a7a 6669 7a7a 6675 7a7a  fizjbuzzfizzfuzz
> 0000030: 4669 7a7a 6275 7a7a 2669 787a 6275 7a7a  Fizzbuzz&ixzbuzz
> 0000040: 6669 7a7a 6275 7a7a 6669 7a7a 6a75 7a7a  fizzbuzzfizzjuzz

Now, let's add a decoder:

> % echo "fizzbuzz" | file\_reconstructor | add\_bit\_errors 0.01 | file\_reconstructor -d | xxd

Despite the bit errors, the data is decoded correctly:

> 0000000: 6669 7a7a 6275 7a7a                      fizzbuzz

To test it with your own files, you can do something like this:

% cat MYFILE | file\_reconstructor | add\_bit\_errors 0.25 | file\_reconstructor -d > NEWFILE
% diff MYFILE NEWFILE

The files should be identical, unless you added too many bit errors for your
level of redundancy, in which case you'll see some errors in the output.

WRITING TO A BLOCK DEVICE
-------------------------

file\_reconstructor is meant to be used with a RAW BLOCK DEVICE. Do not use it
with a file in a filesystem, nor with a partition on a block device, unless you
really understand what you are doing. In most cases, doing that will make it
difficult or impossible to use file\_reconstructor to recover your data in the
event of bit errors which affect the portion of the media containing filesystem
metadata or the partition table.

You copy the output to a block device like this:

> % file\_reconstructor < mydata > /dev/sdc

If you want, you can use a tool like pv to monitor progress:

> % file\_reconstructor < mydata | pv > /dev/sdc

This is only really useful with fast and large media, otherwise the write cache
will fill and pv will have nothing to report.

DISCUSSION AND SIMILAR TOOLS
----------------------------

If you're looking for a similar tool that can recover from a relatively small
number of bit errors with A LOT less redundancy than file\_reconstructor, take
a look at rsbep. In my very inconclusive testing, rsbep could handle something
around a 3% bit error rate with about 10% overhead, but it doesn't appear to be
able to offer more protection than that. I'm not sure whether that's due to
limitations of Reed Solomon codes in general or just rsbep's implementation of
them.

In contrast, this tool can likely handle a 48% error rate with 10000x overhead
or as much as you can stomach, really, but you *must* have at least 3x overhead
to get any protection at all (3x overhead means there will be one tie-breaker
for a single bit error). This is inefficient, yes, but the goal here is
simplicity of the on-disk format and code, not maximum efficiency.

If someone who understands the math better wants to contribute more exact
numbers about this, please feel free :)
